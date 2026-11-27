package telegram

// Встроенный CheckerAbuse: кнопки «Статус / События / Люди» и watchdog цикла проверки.
// Включается, когда включена гео-проверка (GEO_BLOCK) и есть city-база MaxMind.

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/oschwald/maxminddb-golang/v2"

	"github.com/remnawave/limiter/internal/api"
	"github.com/remnawave/limiter/internal/cache"
)

const (
	ckPage      = 8
	ckWorkers   = 8
	ckGreen     = `<tg-emoji emoji-id="5469635405437511533">🟢</tg-emoji>`
	ckMaxAgeMin = 10
)

var msk = time.FixedZone("MSK", 3*3600)

type ckUser struct {
	ID     int64
	Name   string
	Status string
	Expire string
	TG     int64
	Limit  int
	Nodes  map[string][]string
	IPs    map[string]bool
	Cities map[string][]string
	RU     int
	ASN    map[string]ckASN

	Used, TrafficLimit int64
	Devices            []string
	IPNode             map[string]string
}

type ckASN struct {
	Num  uint32
	Org  string
	Kind string
}

type CheckerHealth struct {
	LastCheck time.Time
	Action    string
	Mult      int
}

type CheckerActions struct {
	ASN       func(ip string) (uint32, string, bool)
	Disable   func(ctx context.Context, userID int64) error
	Enable    func(ctx context.Context, userID int64) error
	Whitelist func(ctx context.Context, userID int64) error
	Ban       func(ctx context.Context, telegramID int64) (string, error)
}

type checker struct {
	b      *Bot
	api    *api.Client
	mmdb   string
	act    CheckerActions
	health func() CheckerHealth
	events func(context.Context, time.Duration) ([]cache.ViolationEvent, error)

	mu    sync.Mutex
	users map[int64]*ckUser
}

func (b *Bot) EnableChecker(ctx context.Context, c *api.Client, mmdb string, health func() CheckerHealth, act CheckerActions, limiterEvents func(context.Context, time.Duration) ([]cache.ViolationEvent, error)) {
	if mmdb == "" || health == nil {
		return
	}
	b.ck = &checker{b: b, api: c, events: limiterEvents, mmdb: mmdb, health: health, act: act}
	go b.ck.watchdog(ctx)
}

func (c *checker) mult() int {
	if m := c.health().Mult; m > 0 {
		return m
	}
	return 4
}

func mskTime(t time.Time) string { return t.In(msk).Format("15:04") }

func (c *checker) dbBuilt() string {
	rd, err := maxminddb.Open(c.mmdb)
	if err != nil {
		return ""
	}
	defer rd.Close()
	return rd.Metadata.BuildTime().In(msk).Format("02.01.2006")
}

// ---------- Экраны ----------

func (c *checker) menu(cur string) []richButton {
	all := []richButton{
		cbButton("📊 Статус", "ck:s", styleNone),
		cbButton("⚠️ События", "ck:e", styleNone),
		cbButton("👥 Люди", "ck:l:0", styleNone),
	}
	out := make([]richButton, 0, 3)
	for _, bt := range all {
		if strings.Split(bt.Data, ":")[1] != cur {
			out = append(out, bt)
		}
	}
	return out
}

func (c *checker) extraRow() []richButton {
	return []richButton{cbButton("📈 Статистика", "ui:stats", styleNone), cbButton("⚙️ Настройки лимитера", "cfg:menu", styleNone)}
}

func (c *checker) statusScreen(ctx context.Context) richMsg {
	var h strings.Builder
	h.WriteString("<h3>📊 Статус</h3><table bordered striped compact>")
	hl := c.health()
	var age time.Duration
	ok, text := false, "ждёт первой проверки"
	if !hl.LastCheck.IsZero() {
		age = time.Since(hl.LastCheck)
		if age <= ckMaxAgeMin*time.Minute {
			ok, text = true, "работает"
		} else {
			text = "не отвечает"
		}
	}
	mark := "🔴"
	if ok {
		mark = ckGreen
	}
	h.WriteString("<tr><th align=\"left\">Abuse Control</th><td>" + mark + " " + escapeHTML(text) + "</td></tr>")
	if !hl.LastCheck.IsZero() {
		h.WriteString(tableRow("Последняя проверка", fmt.Sprintf("в %s МСК (%d мин назад)", mskTime(hl.LastCheck), int(age.Minutes()))))
	}
	h.WriteString(tableRow("Гео-проверка", "включена · действие: "+escapeHTML(hl.Action)+fmt.Sprintf(" · порог: городов > лимит × %d", c.mult())))
	if built := c.dbBuilt(); built != "" {
		h.WriteString(tableRow("База городов", "сборка от "+built))
	}
	h.WriteString("</table><footer>обновлено в " + mskTime(time.Now()) + " МСК</footer>")
	rows := [][]richButton{c.menu("s"), {cbButton("🔄 Обновить", "ck:s", stylePrimary)}, c.extraRow()}
	return richMsg{HTML: h.String(), Rows: rows}
}

func (c *checker) eventsScreen(ctx context.Context) richMsg {
	var ev []string
	type item struct {
		t time.Time
		s string
	}
	var all []item
	if c.events != nil {
		if le, err := c.events(ctx, 24*time.Hour); err != nil {
			ev = append(ev, "Redis недоступен: "+err.Error())
		} else {
			for _, e := range le {
				who := e.Username
				if who == "" {
					who = "id " + e.UserID
				}
				all = append(all, item{e.At, "🚫 " + mskTime(e.At) + " " + who})
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].t.Before(all[j].t) })
	for _, it := range all {
		ev = append(ev, it.s)
	}
	if len(ev) > 15 {
		ev = ev[len(ev)-15:]
	}
	h := "<h3>⚠️ События за 24 часа</h3>"
	if len(ev) == 0 {
		h += "<p>нарушений не было</p>"
	} else {
		h += "<blockquote>"
		for _, e := range ev {
			h += escapeHTML(e) + "<br>"
		}
		h += "</blockquote>"
	}
	return richMsg{HTML: h, Rows: [][]richButton{c.menu("e"), c.extraRow()}}
}

// ---------- Люди ----------

func (c *checker) lookup(r *maxminddb.Reader, ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return "Unknown"
	}
	var rec struct {
		City struct {
			Names map[string]string `maxminddb:"names"`
		} `maxminddb:"city"`
		Country struct {
			ISO string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	_ = r.Lookup(a).Decode(&rec)
	city := rec.City.Names["en"]
	if city == "" {
		city = rec.City.Names["ru"]
	}
	if city == "" {
		city = "Unknown"
	}
	cc := rec.Country.ISO
	if cc == "" {
		cc = "?"
	}
	if cc == "RU" {
		return city
	}
	return fmt.Sprintf("%s (%s)", city, cc)
}

func (c *checker) refresh(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	nodes, err := c.api.GetNodes(ctx)
	if err != nil {
		return err
	}
	type res struct {
		name string
		ents []api.UserIPEntry
	}
	ch := make(chan res)
	n := 0
	for _, nd := range nodes {
		skip := nd.IsDisabled || !nd.IsConnected
		for _, t := range nd.Tags {
			skip = skip || t == "NO_REAL_IP"
		}
		if skip {
			continue
		}
		n++
		go func(nd api.Node) {
			e, err := c.api.FetchUsersIPs(ctx, nd.UUID)
			if err != nil {
				c.b.logger.WithError(err).WithField("node", nd.Name).Warn("checker: node ips")
			}
			ch <- res{nd.Name, e}
		}(nd)
	}
	users := map[int64]*ckUser{}
	for ; n > 0; n-- {
		r := <-ch
		for _, e := range r.ents {
			u := users[e.UserID]
			if u == nil {
				u = &ckUser{ID: e.UserID, Nodes: map[string][]string{}, IPs: map[string]bool{}, Cities: map[string][]string{}}
				users[e.UserID] = u
			}
			for _, ip := range e.IPs {
				u.IPs[ip.IP] = true
				if u.IPNode == nil {
					u.IPNode = map[string]string{}
				}
				u.IPNode[ip.IP] = r.name
				u.Nodes[r.name] = append(u.Nodes[r.name], ip.IP)
			}
		}
	}
	rd, err := maxminddb.Open(c.mmdb)
	if err != nil {
		return err
	}
	defer rd.Close()
	// карточки пользователей запрашиваем параллельно (не больше ckWorkers),
	// каждая горутина пишет только в своего ckUser
	var wg sync.WaitGroup
	sem := make(chan struct{}, ckWorkers)
	for id, u := range users {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			c.fill(ctx, rd, id, u)
		}()
	}
	wg.Wait()
	c.mu.Lock()
	c.users = users
	c.mu.Unlock()
	return nil
}

// fill дополняет пользователя данными панели и городами по IP.
func (c *checker) fill(ctx context.Context, rd *maxminddb.Reader, id int64, u *ckUser) {
	u.Name, u.Status = fmt.Sprintf("id %d", id), "?"
	if d, err := c.api.GetUserByID(ctx, id); err == nil && d != nil {
		if d.Username != "" {
			u.Name = d.Username
		}
		u.Status = d.Status
		if d.HWIDDeviceLimit != nil {
			u.Limit = *d.HWIDDeviceLimit
		}
		if d.TelegramID != nil {
			u.TG = *d.TelegramID
		}
		if d.ExpireAt != nil {
			u.Expire = d.ExpireAt.Format("02.01.2006")
		}
		u.Used, u.TrafficLimit = d.UserTraffic.UsedTrafficBytes, d.TrafficLimitBytes
	}
	if devs, err := c.api.GetUserHWIDDevices(ctx, id); err == nil {
		for _, dv := range devs {
			u.Devices = append(u.Devices, deviceLabel(dv))
		}
	}
	u.ASN = map[string]ckASN{}
	for ip := range u.IPs {
		city := c.lookup(rd, ip)
		u.Cities[city] = append(u.Cities[city], ip)
		if c.act.ASN != nil {
			if num, org, ok := c.act.ASN(ip); ok {
				u.ASN[ip] = ckASN{Num: num, Org: org, Kind: netKind(org)}
			}
		}
	}
	// «городов» = российские города (без суффикса страны)
	for city := range u.Cities {
		if !strings.HasSuffix(city, ")") {
			u.RU++
		}
	}
}

func (c *checker) sorted() []*ckUser {
	c.mu.Lock()
	defer c.mu.Unlock()
	us := make([]*ckUser, 0, len(c.users))
	for _, u := range c.users {
		us = append(us, u)
	}
	sort.Slice(us, func(i, j int) bool {
		if us[i].RU != us[j].RU {
			return us[i].RU > us[j].RU
		}
		if len(us[i].IPs) != len(us[j].IPs) {
			return len(us[i].IPs) > len(us[j].IPs)
		}
		return us[i].ID < us[j].ID
	})
	return us
}

func (c *checker) listScreen(p int) richMsg {
	us := c.sorted()
	pages := (len(us) + ckPage - 1) / ckPage
	if pages < 1 {
		pages = 1
	}
	p = min(max(p, 0), pages-1)
	var rows [][]richButton
	for _, u := range us[min(p*ckPage, len(us)):min((p+1)*ckPage, len(us))] {
		ic := "👤 "
		if u.Limit > 0 && u.RU > u.Limit*c.mult() {
			ic = "⚠️ "
		}
		rows = append(rows, []richButton{cbButton(fmt.Sprintf("%s%s · %d IP · %d гор.", ic, strings.TrimPrefix(u.Name, "vpn_"), len(u.IPs), u.RU), fmt.Sprintf("ck:u:%d:%d", u.ID, p), styleNone)})
	}
	var nav []richButton
	if p > 0 {
		nav = append(nav, cbButton("◀️", fmt.Sprintf("ck:l:%d", p-1), styleNone))
	}
	if p < pages-1 {
		nav = append(nav, cbButton("▶️", fmt.Sprintf("ck:l:%d", p+1), styleNone))
	}
	if len(nav) > 0 {
		rows = append(rows, nav)
	}
	rows = append(rows, []richButton{cbButton("⬅️ Назад", "ck:s", styleNone), cbButton("🔄 Обновить", fmt.Sprintf("ck:r:%d", p), stylePrimary)})
	return richMsg{HTML: fmt.Sprintf("<h3>👥 Сейчас онлайн: %d</h3>", len(us)), Rows: rows}
}

func tgEmoji(id, ch string) string { return `<tg-emoji emoji-id="` + id + `">` + ch + `</tg-emoji>` }

var ckStatus = map[string]string{"ACTIVE": ckGreen + " Активен", "DISABLED": "🔴 Отключён", "LIMITED": "🟠 Лимит трафика", "EXPIRED": "⚫ Истёк", "BANNED": "⛔ Удалён и забанен"}

func (c *checker) cardScreen(uid int64, p int) richMsg {
	back := cbButton("⬅️ Назад", fmt.Sprintf("ck:l:%d", p), styleNone)
	c.mu.Lock()
	u := c.users[uid]
	c.mu.Unlock()
	if u == nil {
		return richMsg{HTML: "<p>Пользователь сейчас не подключён.</p>", Rows: [][]richButton{{back}}}
	}
	st := ckStatus[u.Status]
	if st == "" {
		st = escapeHTML(u.Status)
	}
	if u.Expire != "" {
		st += " · до " + u.Expire
	}
	lim := "—"
	ban := ""
	if u.Limit > 0 {
		lim = strconv.Itoa(u.Limit)
		ban = fmt.Sprintf(" (блок при &gt; %d)", u.Limit*c.mult())
	}
	var h strings.Builder
	h.WriteString("<h3>" + st + "</h3><table bordered striped compact>")
	h.WriteString("<tr><th align=\"left\">" + tgEmoji("5275979556308674886", "👤") + " Username</th><td><code>" + escapeHTML(u.Name) + "</code></td></tr>")
	if u.TG != 0 {
		h.WriteString("<tr><th align=\"left\">" + tgEmoji("5285350148451344065", "💬") + " Telegram id</th><td><code>" + strconv.FormatInt(u.TG, 10) + "</code></td></tr>")
	}
	h.WriteString("<tr><th align=\"left\">" + tgEmoji("5278305362703835500", "🔗") + " Сессий</th><td>" + strconv.Itoa(len(u.IPs)) + "</td></tr>")
	h.WriteString("<tr><th align=\"left\">" + tgEmoji("6048522487845621737", "📱") + " Лимит устройств</th><td>" + lim + "</td></tr>")
	h.WriteString("<tr><th align=\"left\">" + tgEmoji("6048572296581353415", "📊") + " Городов</th><td>" + strconv.Itoa(u.RU) + ban + "</td></tr>")
	if len(u.ASN) > 0 {
		nums := map[uint32]bool{}
		dc := 0
		for _, a := range u.ASN {
			nums[a.Num] = true
			if a.Kind == kindDC {
				dc++
			}
		}
		h.WriteString("<tr><th align=\"left\">Провайдеров</th><td>" + strconv.Itoa(len(nums)) + "</td></tr>")
		if dc > 0 {
			h.WriteString("<tr><th align=\"left\">Дата-центр</th><td>" + strconv.Itoa(dc) + " IP: похоже на прокси или перепродажу</td></tr>")
		}
	}
	if t := trafficText(u.Used, u.TrafficLimit); t != "" {
		h.WriteString("<tr><th align=\"left\">Трафик</th><td>" + t + "</td></tr>")
	}
	if len(u.Devices) > 0 {
		h.WriteString("<tr><th align=\"left\">Устройства</th><td>" + escapeHTML(groupDevices(u.Devices)) + "</td></tr>")
	}
	h.WriteString("</table>")
	h.WriteString("<p><b>" + tgEmoji("5276314275994954605", "🔨") + " Список городов:</b></p><blockquote>")
	cities := make([]string, 0, len(u.Cities))
	for city := range u.Cities {
		cities = append(cities, city)
	}
	sort.Strings(cities)
	n := 0
	for _, city := range cities {
		ips := append([]string(nil), u.Cities[city]...)
		sort.Strings(ips)
		for _, ip := range ips {
			if n++; n > maxIPRows {
				break
			}
			a, known := u.ASN[ip]
			suffix := ""
			if len(u.ASN) > 0 {
				suffix = netSuffix(a, known)
			}
			h.WriteString(`<a href="https://ipinfo.io/` + escapeHTML(ip) + `">` + escapeHTML(city) + `</a> · <code>` + escapeHTML(ip) + `</code>` + suffix + nodeSuffix(u, ip) + `<br>`)
		}
	}
	h.WriteString("</blockquote>")
	nodes := make([]string, 0, len(u.Nodes))
	for name, ips := range u.Nodes {
		nodes = append(nodes, fmt.Sprintf("%s (%d)", shortNode(name), len(ips)))
	}
	sort.Strings(nodes)
	h.WriteString("<p><b>🖥 Ноды:</b> " + escapeHTML(strings.Join(nodes, ", ")) + "</p>")
	if len(u.ASN) > 0 {
		h.WriteString("<footer>тип сети определён по названию провайдера, примерно</footer>")
	}
	rows := c.actionRows(u, p)
	rows = append(rows, []richButton{back, cbButton("🔄", fmt.Sprintf("ck:u:%d:%d", uid, p), styleNone)})
	return richMsg{HTML: h.String(), Rows: rows}
}

// ---------- Обработчики ----------

func (c *checker) handleCallback(ctx context.Context, cb *models.CallbackQuery) {
	parts := strings.Split(cb.Data, ":")
	chatID, msgID, _ := callbackMessage(cb)
	if len(parts) < 2 {
		c.b.answerCallback(ctx, cb.ID, "")
		return
	}
	arg := func(i int) int64 {
		if i < len(parts) {
			v, _ := strconv.ParseInt(parts[i], 10, 64)
			return v
		}
		return 0
	}
	switch parts[1] {
	case "d", "n", "w", "B":
		text := c.doAction(ctx, parts[1], arg(2))
		c.b.answerCallback(ctx, cb.ID, text)
		if err := c.b.editRich(ctx, chatID, msgID, c.cardScreen(arg(2), int(arg(3)))); err != nil {
			c.b.logger.WithError(err).Warn("checker: edit")
		}
		return
	case "b":
		c.b.answerCallback(ctx, cb.ID, "")
		if err := c.b.editRich(ctx, chatID, msgID, c.banConfirmScreen(arg(2), int(arg(3)))); err != nil {
			c.b.logger.WithError(err).Warn("checker: edit")
		}
		return
	}
	if parts[1] == "r" {
		c.b.answerCallback(ctx, cb.ID, "Обновляю, до 30 сек…")
		if err := c.refresh(ctx); err != nil {
			c.b.logger.WithError(err).Warn("checker: refresh")
		}
	} else {
		c.b.answerCallback(ctx, cb.ID, "")
		c.mu.Lock()
		empty := c.users == nil
		c.mu.Unlock()
		if empty && (parts[1] == "l" || parts[1] == "u") {
			if err := c.refresh(ctx); err != nil {
				c.b.logger.WithError(err).Warn("checker: refresh")
			}
		}
	}
	var m richMsg
	switch parts[1] {
	case "e":
		m = c.eventsScreen(ctx)
	case "l", "r":
		m = c.listScreen(int(arg(2)))
	case "u":
		m = c.cardScreen(arg(2), int(arg(3)))
	default:
		m = c.statusScreen(ctx)
	}
	if err := c.b.editRich(ctx, chatID, msgID, m); err != nil {
		c.b.logger.WithError(err).Warn("checker: edit")
	}
}

// home отправляет главное сообщение со статусом и кнопками.
func (c *checker) home(ctx context.Context, chatID int64, threadID int) {
	if _, err := c.b.sendRich(ctx, chatID, threadID, c.statusScreen(ctx)); err != nil {
		c.b.logger.WithError(err).Warn("checker: home")
	}
}

func (c *checker) watchdog(ctx context.Context) {
	started := time.Now()
	bad := false
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		last := c.health().LastCheck
		ref := last
		if ref.IsZero() {
			ref = started
		}
		prob := ""
		if age := time.Since(ref); age > ckMaxAgeMin*time.Minute {
			prob = fmt.Sprintf("нет успешной проверки %d мин", int(age.Minutes()))
		}
		var msg string
		switch {
		case prob != "" && !bad:
			msg = alertProblem(prob)
		case prob == "" && bad:
			msg = alertRecovered()
		}
		bad = prob != ""
		if msg != "" {
			_ = c.b.sendMsg(ctx, richMsg{HTML: msg})
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func alertProblem(p string) string {
	return "<p>🔴 <b>Abuse Control</b>: " + escapeHTML(p) + "</p>"
}
func alertRecovered() string {
	return "<p>" + ckGreen + " <b>Abuse Control</b> снова работает</p>"
}

// TestAlerts шлёт образцы всех алертов (команда /alerttest). Кнопки ведут на несуществующего пользователя.
func (b *Bot) TestAlerts(ctx context.Context) {
	loc := b.location
	if loc == nil {
		loc = time.UTC
	}
	u := &api.CachedUser{UserID: 999999999, Username: "test_user", HWIDDeviceLimit: 2}
	ips := []api.ActiveIP{
		{IP: "203.0.113.10", LastSeen: time.Now(), NodeName: "node-1"},
		{IP: "198.51.100.7", LastSeen: time.Now(), NodeName: "node-2"},
		{IP: "192.0.2.55", LastSeen: time.Now(), NodeName: "node-3"},
	}
	_ = b.SendSoftAlert(ctx, "<p>🧪 <b>ТЕСТ</b></p>"+FormatSoftAlert(u, ips, 2, 4, loc, 3, false, 3, false, nil), u.UserID, 60)
	_ = b.SendManualAlert(ctx, "<p>🧪 <b>ТЕСТ</b></p>"+FormatManualAlert(u, ips, 2, 3, loc, 3, false, 3, false, nil), u.UserID, 60, 60)
	_ = b.SendAutoAlert(ctx, "<p>🧪 <b>ТЕСТ</b></p>"+FormatAutoAlert(u, ips, 2, 60, 3, loc, 3, false, 3, false, nil), u.UserID)
	_ = b.sendMsg(ctx, richMsg{HTML: "<p>🧪 <b>ТЕСТ</b></p>" + alertProblem("нет успешной проверки 15 мин")})
	_ = b.sendMsg(ctx, richMsg{HTML: "<p>🧪 <b>ТЕСТ</b></p>" + alertRecovered()})
}

func (c *checker) actionRows(u *ckUser, p int) [][]richButton {
	if u.Status == "BANNED" {
		return nil
	}
	var rows [][]richButton
	var first []richButton
	if u.Status == "DISABLED" {
		if c.act.Enable != nil {
			first = append(first, cbButton("🔓 Включить", fmt.Sprintf("ck:n:%d:%d", u.ID, p), stylePrimary))
		}
	} else if c.act.Disable != nil {
		first = append(first, cbButton("🔒 Отключить", fmt.Sprintf("ck:d:%d:%d", u.ID, p), styleNone))
	}
	if c.act.Whitelist != nil {
		first = append(first, cbButton("✅ В белый список", fmt.Sprintf("ck:w:%d:%d", u.ID, p), styleNone))
	}
	if len(first) > 0 {
		rows = append(rows, first)
	}
	if c.act.Ban != nil && u.TG != 0 {
		rows = append(rows, []richButton{cbButton("🚫 Удалить и забанить", fmt.Sprintf("ck:b:%d:%d", u.ID, p), styleDanger)})
	}
	return rows
}

func (c *checker) userByID(uid int64) *ckUser {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.users[uid]
}

func (c *checker) setStatus(uid int64, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if u := c.users[uid]; u != nil {
		u.Status = status
	}
}

func (c *checker) banConfirmScreen(uid int64, p int) richMsg {
	u := c.userByID(uid)
	if u == nil || c.act.Ban == nil || u.TG == 0 {
		return richMsg{HTML: "<p>Пользователь сейчас не подключён.</p>", Rows: [][]richButton{{cbButton("⬅️ Назад", fmt.Sprintf("ck:l:%d", p), styleNone)}}}
	}
	h := "<h3>🚫 Удалить и забанить?</h3><p><code>" + escapeHTML(u.Name) + "</code> · Telegram id <code>" + strconv.FormatInt(u.TG, 10) + "</code></p>" +
		"<p>Бот TunnelKey сбросит доступ, отменит автопродление и забанит. Подписка пропадёт без возврата. Если человек когда-либо платил, бот ничего не тронет.</p>"
	return richMsg{HTML: h, Rows: [][]richButton{{
		cbButton("🚫 Да, забанить", fmt.Sprintf("ck:B:%d:%d", uid, p), styleDanger),
		cbButton("Отмена", fmt.Sprintf("ck:u:%d:%d", uid, p), styleNone),
	}}}
}

func (c *checker) doAction(ctx context.Context, code string, uid int64) string {
	u := c.userByID(uid)
	if u == nil {
		return "Пользователь сейчас не подключён"
	}
	switch code {
	case "d":
		if c.act.Disable == nil {
			return "Недоступно"
		}
		if err := c.act.Disable(ctx, uid); err != nil && !api.IsAlreadyDisabled(err) {
			return "Ошибка: " + err.Error()
		}
		c.setStatus(uid, "DISABLED")
		return "Отключён"
	case "n":
		if c.act.Enable == nil {
			return "Недоступно"
		}
		if err := c.act.Enable(ctx, uid); err != nil && !api.IsAlreadyEnabled(err) {
			return "Ошибка: " + err.Error()
		}
		c.setStatus(uid, "ACTIVE")
		return "Включён"
	case "w":
		if c.act.Whitelist == nil {
			return "Недоступно"
		}
		if err := c.act.Whitelist(ctx, uid); err != nil {
			return "Ошибка: " + err.Error()
		}
		return "В белом списке: лимитер его больше не проверяет"
	case "B":
		if c.act.Ban == nil || u.TG == 0 {
			return "Недоступно: нет Telegram id или связи с ботом"
		}
		res, err := c.act.Ban(ctx, u.TG)
		if err != nil {
			return "Ошибка связи с ботом: " + err.Error()
		}
		switch res {
		case "banned":
			c.setStatus(uid, "BANNED")
			return "Удалён и забанен"
		case "paid":
			return "Платил: бот не тронул. Банить платящего — в боте, «Доступ → Бан»"
		case "already_banned":
			return "Уже забанен в боте"
		case "unknown":
			return "Бот не знает такого пользователя"
		}
		return "Ответ бота: " + res
	}
	return ""
}

const (
	kindDC     = "dc"
	kindMobile = "mobile"
)

func netLabel(kind string) string {
	switch kind {
	case kindMobile:
		return "LTE"
	case kindDC:
		return "Дата-центр"
	}
	return "Другое"
}

func netSuffix(a ckASN, known bool) string {
	if !known || a.Num == 0 {
		return " · Неизвестно"
	}
	name := shortOrg(a.Org)
	if name == "" {
		name = "AS" + strconv.FormatUint(uint64(a.Num), 10)
	}
	return " · " + escapeHTML(name) + " (" + netLabel(a.Kind) + ")"
}

var orgAliases = []struct{ match, name string }{
	{"vimpelcom", "Beeline"},
	{"moscow city telephone", "МГТС"},
	{"megafon", "MegaFon"},
	{"tele2", "Tele2"},
	{"rostelecom", "Rostelecom"},
	{"er-telecom", "ER-Telecom"},
	{"transtelecom", "TTK"},
	{"yota", "Yota"},
	{"hetzner", "Hetzner"},
	{"digitalocean", "DigitalOcean"},
	{"google", "Google"},
	{"amazon", "Amazon"},
	{"microsoft", "Microsoft"},
}

var orgNoise = map[string]bool{
	"pjsc": true, "jsc": true, "ojsc": true, "pao": true, "oao": true, "ooo": true, "zao": true,
	"llc": true, "ltd": true, "inc": true, "gmbh": true, "srl": true, "sas": true, "ag": true,
	"co": true, "corp": true, "company": true, "public": true, "joint": true, "stock": true,
}

const orgMaxRunes = 18

func shortOrg(org string) string {
	l := strings.ToLower(org)
	for _, al := range orgAliases {
		if strings.Contains(l, al.match) {
			return al.name
		}
	}
	if toks := strings.Fields(l); len(toks) == 1 && toks[0] == "mts" {
		return "MTS"
	}
	var kept []string
	for _, w := range strings.Fields(org) {
		if orgNoise[strings.ToLower(strings.Trim(w, ".,"))] {
			continue
		}
		kept = append(kept, strings.Trim(w, ","))
	}
	out := strings.Join(kept, " ")
	if r := []rune(out); len(r) > orgMaxRunes {
		cut := string(r[:orgMaxRunes])
		if r[orgMaxRunes] != ' ' {
			if i := strings.LastIndex(cut, " "); i > 5 {
				cut = cut[:i]
			}
		}
		out = cut
	}
	return out
}

var (
	dcTokens     = map[string]bool{"aws": true, "ovh": true, "vps": true, "ihc": true, "edis": true, "gcp": true, "m247": true, "aeza": true, "ec2": true}
	dcPhrases    = []string{"hosting", "cloud", "datacenter", "data center", "dedicated", "digitalocean", "hetzner", "amazon", "google", "microsoft", "azure", "leaseweb", "contabo", "linode", "vultr", "choopa", "scaleway", "oracle", "alibaba", "tencent", "selectel", "timeweb", "beget", "vdsina", "firstbyte", "hostkey", "clouvider", "datacamp", "cdn77", "zenlayer", "equinix", "frantech", "quadranet", "psychz", "server", "colo"}
	mobileTokens = map[string]bool{"mts": true, "lte": true, "yota": true, "tele2": true, "t2": true}
	mobPhrases   = []string{"mobile", "cellular", "wireless", "megafon", "vimpelcom", "beeline", "motiv"}
)

func netKind(org string) string {
	l := strings.ToLower(org)
	toks := map[string]bool{}
	for _, t := range strings.FieldsFunc(l, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		toks[t] = true
	}
	for t := range toks {
		if dcTokens[t] {
			return kindDC
		}
	}
	for _, ph := range dcPhrases {
		if strings.Contains(l, ph) {
			return kindDC
		}
	}
	for t := range toks {
		if mobileTokens[t] {
			return kindMobile
		}
	}
	for _, ph := range mobPhrases {
		if strings.Contains(l, ph) {
			return kindMobile
		}
	}
	return ""
}

func deviceLabel(d api.HWIDDevice) string {
	plat := strings.TrimSpace(d.Platform)
	if plat == "" {
		plat = "?"
	}
	if d.DeviceModel != "" {
		plat = d.DeviceModel
	}
	if app := appFromUA(d.UserAgent); app != "" {
		return plat + " (" + app + ")"
	}
	return plat
}

func appFromUA(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" || strings.HasPrefix(ua, "Mozilla/") {
		return ""
	}
	name, _, _ := strings.Cut(ua, "/")
	name, _, _ = strings.Cut(name, " ")
	return name
}

func formatBytes(n int64) string {
	const gb = 1 << 30
	if n >= gb {
		return strconv.FormatFloat(float64(n)/gb, 'f', 1, 64) + " ГБ"
	}
	return strconv.FormatInt(n>>20, 10) + " МБ"
}

func trafficText(used, limit int64) string {
	if used <= 0 && limit <= 0 {
		return ""
	}
	if limit <= 0 {
		return formatBytes(used)
	}
	return formatBytes(used) + " из " + formatBytes(limit)
}

func groupDevices(devs []string) string {
	type grp struct {
		n    int
		apps []string
	}
	groups := map[string]*grp{}
	var order []string
	for _, d := range devs {
		plat, app, _ := strings.Cut(d, " (")
		app = strings.TrimSuffix(app, ")")
		g := groups[plat]
		if g == nil {
			g = &grp{}
			groups[plat] = g
			order = append(order, plat)
		}
		g.n++
		if app != "" && !slices.Contains(g.apps, app) {
			g.apps = append(g.apps, app)
		}
	}
	out := make([]string, 0, len(order))
	for _, plat := range order {
		g := groups[plat]
		s := plat
		if g.n > 1 {
			s += " ×" + strconv.Itoa(g.n)
		}
		if len(g.apps) > 0 {
			s += " (" + strings.Join(g.apps, ", ") + ")"
		}
		out = append(out, s)
	}
	return strings.Join(out, ", ")
}

func shortNode(name string) string {
	base, ok := strings.CutSuffix(name, "-Server")
	if !ok || base == "" {
		return name
	}
	r := []rune(strings.ToUpper(base))
	if len(r) <= 2 {
		return string(r)
	}
	for _, c := range r[1:] {
		if !strings.ContainsRune("AEIOUY", c) && c >= 'A' && c <= 'Z' {
			return string(r[0]) + string(c)
		}
	}
	return string(r[:2])
}

func nodeSuffix(u *ckUser, ip string) string {
	if n := u.IPNode[ip]; n != "" {
		return " · " + escapeHTML(shortNode(n))
	}
	return ""
}
