package telegram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/remnawave/limiter/internal/api"
)

func TestNetKind(t *testing.T) {
	cases := map[string]string{
		"MTS PJSC":                       kindMobile,
		"PJSC MegaFon":                   kindMobile,
		"VimpelCom":                      kindMobile,
		"Tele2 Russia":                   kindMobile,
		"Hetzner Online GmbH":            kindDC,
		"DigitalOcean, LLC":              kindDC,
		"OVH SAS":                        kindDC,
		"Amazon.com, Inc.":               kindDC,
		"M247 Europe SRL":                kindDC,
		"Selectel Ltd":                   kindDC,
		"Rostelecom":                     "",
		"ER-Telecom Holding":             "",
		"":                               "",
		"Smart Telecom":                  "",
		"HOSTING-SERVICES Inc":           kindDC,
		"MTS Mobile":                     kindMobile,
		"Hostmaster mts reseller server": kindDC,
	}
	for org, want := range cases {
		if got := netKind(org); got != want {
			t.Errorf("netKind(%q)=%q, want %q", org, got, want)
		}
	}
}

func TestShortOrg(t *testing.T) {
	cases := map[string]string{
		"PVimpelCom":                         "Beeline",
		"PJSC Moscow city telephone network": "МГТС",
		"MTS PJSC":                           "MTS",
		"PJSC MegaFon":                       "MegaFon",
		"Rostelecom":                         "Rostelecom",
		"Hetzner Online GmbH":                "Hetzner",
		"Some Small Provider LLC":            "Some Small",
		"OOO Very Long Provider Name Group":  "Very Long Provider",
		"":                                   "",
	}
	for in, want := range cases {
		if got := shortOrg(in); got != want {
			t.Errorf("shortOrg(%q)=%q, want %q", in, got, want)
		}
	}
	if r := []rune(shortOrg(strings.Repeat("я", 60))); len(r) > orgMaxRunes {
		t.Errorf("длинное название не обрезано: %d", len(r))
	}
}

func TestNetSuffix(t *testing.T) {
	cases := []struct {
		a     ckASN
		known bool
		want  string
	}{
		{ckASN{}, false, " · Неизвестно"},
		{ckASN{}, true, " · Неизвестно"},
		{ckASN{Num: 16345, Org: "PVimpelCom", Kind: kindMobile}, true, " · Beeline (LTE)"},
		{ckASN{Num: 25513, Org: "PJSC Moscow city telephone network"}, true, " · МГТС (Другое)"},
		{ckASN{Num: 24940, Org: "Hetzner Online GmbH", Kind: kindDC}, true, " · Hetzner (Дата-центр)"},
		{ckASN{Num: 64500, Org: "<b>"}, true, " · &lt;b&gt; (Другое)"},
		{ckASN{Num: 64500, Org: "LLC"}, true, " · AS64500 (Другое)"},
	}
	for _, tc := range cases {
		if got := netSuffix(tc.a, tc.known); got != tc.want {
			t.Errorf("netSuffix(%+v,%v)=%q, want %q", tc.a, tc.known, got, tc.want)
		}
	}
}

func TestNetLabel(t *testing.T) {
	if netLabel(kindMobile) != "LTE" || netLabel(kindDC) != "Дата-центр" || netLabel("") != "Другое" {
		t.Fatal("названия типов сети перепутаны")
	}
}

func cardChecker(act CheckerActions, u *ckUser) *checker {
	c := &checker{act: act, health: func() CheckerHealth { return CheckerHealth{Mult: 4} }, users: map[int64]*ckUser{u.ID: u}}
	return c
}

func baseUser() *ckUser {
	return &ckUser{
		ID: 5, Name: "vpn_x", Status: "ACTIVE", TG: 777, Limit: 5,
		Nodes:  map[string][]string{"n1": {"1.1.1.1"}},
		IPs:    map[string]bool{"1.1.1.1": true, "2.2.2.2": true},
		Cities: map[string][]string{"Moscow": {"1.1.1.1"}, "Kazan": {"2.2.2.2"}},
		RU:     2,
		ASN: map[string]ckASN{
			"1.1.1.1": {Num: 8359, Org: "MTS PJSC", Kind: kindMobile},
			"2.2.2.2": {Num: 24940, Org: "Hetzner Online GmbH", Kind: kindDC},
		},
	}
}

func buttonsData(m richMsg) []string {
	var out []string
	for _, row := range m.Rows {
		for _, b := range row {
			out = append(out, b.Data)
		}
	}
	return out
}

func hasData(m richMsg, prefix string) bool {
	for _, d := range buttonsData(m) {
		if strings.HasPrefix(d, prefix) {
			return true
		}
	}
	return false
}

func fullActions() CheckerActions {
	return CheckerActions{
		Disable:   func(context.Context, int64) error { return nil },
		Enable:    func(context.Context, int64) error { return nil },
		Whitelist: func(context.Context, int64) error { return nil },
		Ban:       func(context.Context, int64) (string, error) { return "banned", nil },
	}
}

func TestCardShowsProvidersAndDatacenter(t *testing.T) {
	c := cardChecker(fullActions(), baseUser())
	h := c.cardScreen(5, 0).HTML
	for _, w := range []string{"· MTS (LTE)", "· Hetzner (Дата-центр)", "Провайдеров", "по названию провайдера"} {
		if !strings.Contains(h, w) {
			t.Errorf("нет %q в %s", w, h)
		}
	}
}

func TestCardWithoutASNHasNoProviderRows(t *testing.T) {
	u := baseUser()
	u.ASN = nil
	h := cardChecker(fullActions(), u).cardScreen(5, 0).HTML
	if strings.Contains(h, "Провайдеров") || strings.Contains(h, "(LTE)") || strings.Contains(h, "(Другое)") {
		t.Fatalf("лишнее: %s", h)
	}
}

func TestCardButtons(t *testing.T) {
	m := cardChecker(fullActions(), baseUser()).cardScreen(5, 2)
	for _, p := range []string{"ck:d:5:2", "ck:w:5:2", "ck:b:5:2", "ck:l:2"} {
		if !hasData(m, p) {
			t.Errorf("нет кнопки %s: %v", p, buttonsData(m))
		}
	}
	if hasData(m, "ck:n:") || hasData(m, "ck:B:") {
		t.Errorf("лишние кнопки: %v", buttonsData(m))
	}

	u := baseUser()
	u.Status = "DISABLED"
	m = cardChecker(fullActions(), u).cardScreen(5, 0)
	if !hasData(m, "ck:n:5:0") || hasData(m, "ck:d:") {
		t.Errorf("у отключённого должна быть «Включить»: %v", buttonsData(m))
	}

	u = baseUser()
	u.Status = "BANNED"
	m = cardChecker(fullActions(), u).cardScreen(5, 0)
	if hasData(m, "ck:d:") || hasData(m, "ck:n:") || hasData(m, "ck:w:") || hasData(m, "ck:b:") {
		t.Errorf("у забаненного действий быть не должно: %v", buttonsData(m))
	}

	u = baseUser()
	u.TG = 0
	if hasData(cardChecker(fullActions(), u).cardScreen(5, 0), "ck:b:") {
		t.Error("без Telegram id бана быть не должно")
	}

	act := fullActions()
	act.Ban = nil
	if hasData(cardChecker(act, baseUser()).cardScreen(5, 0), "ck:b:") {
		t.Error("без связи с ботом бана быть не должно")
	}

	m = cardChecker(CheckerActions{}, baseUser()).cardScreen(5, 0)
	if hasData(m, "ck:d:") || hasData(m, "ck:w:") || hasData(m, "ck:b:") {
		t.Errorf("без действий кнопок быть не должно: %v", buttonsData(m))
	}
}

func TestDoActionDisableEnable(t *testing.T) {
	var disabled, enabled int
	act := fullActions()
	act.Disable = func(context.Context, int64) error { disabled++; return nil }
	act.Enable = func(context.Context, int64) error { enabled++; return nil }
	c := cardChecker(act, baseUser())

	if txt := c.doAction(context.Background(), "d", 5); txt != "Отключён" || disabled != 1 {
		t.Fatalf("txt=%q disabled=%d", txt, disabled)
	}
	if c.userByID(5).Status != "DISABLED" {
		t.Fatal("статус не обновился")
	}
	if txt := c.doAction(context.Background(), "n", 5); txt != "Включён" || enabled != 1 {
		t.Fatalf("txt=%q enabled=%d", txt, enabled)
	}
	if c.userByID(5).Status != "ACTIVE" {
		t.Fatal("статус не вернулся")
	}
}

func TestDoActionErrorKeepsStatus(t *testing.T) {
	act := fullActions()
	act.Disable = func(context.Context, int64) error { return errors.New("panel down") }
	c := cardChecker(act, baseUser())
	if txt := c.doAction(context.Background(), "d", 5); !strings.HasPrefix(txt, "Ошибка") {
		t.Fatalf("txt=%q", txt)
	}
	if c.userByID(5).Status != "ACTIVE" {
		t.Fatal("статус не должен меняться при ошибке")
	}
}

func TestDoActionWhitelist(t *testing.T) {
	var got int64
	act := fullActions()
	act.Whitelist = func(_ context.Context, id int64) error { got = id; return nil }
	c := cardChecker(act, baseUser())
	if txt := c.doAction(context.Background(), "w", 5); !strings.Contains(txt, "белом списке") || got != 5 {
		t.Fatalf("txt=%q got=%d", txt, got)
	}
}

func TestDoActionBanResults(t *testing.T) {
	cases := map[string]string{
		"banned":         "Удалён и забанен",
		"paid":           "Платил",
		"already_banned": "Уже забанен",
		"unknown":        "не знает",
		"weird":          "Ответ бота: weird",
	}
	for res, want := range cases {
		var tg int64
		act := fullActions()
		act.Ban = func(_ context.Context, id int64) (string, error) { tg = id; return res, nil }
		c := cardChecker(act, baseUser())
		txt := c.doAction(context.Background(), "B", 5)
		if !strings.Contains(txt, want) || tg != 777 {
			t.Errorf("%s: txt=%q tg=%d", res, txt, tg)
		}
		wantStatus := "ACTIVE"
		if res == "banned" {
			wantStatus = "BANNED"
		}
		if c.userByID(5).Status != wantStatus {
			t.Errorf("%s: статус %q", res, c.userByID(5).Status)
		}
	}

	act := fullActions()
	act.Ban = func(context.Context, int64) (string, error) { return "", errors.New("down") }
	c := cardChecker(act, baseUser())
	if txt := c.doAction(context.Background(), "B", 5); !strings.Contains(txt, "Ошибка связи") {
		t.Fatalf("txt=%q", txt)
	}
	if c.userByID(5).Status != "ACTIVE" {
		t.Fatal("статус не должен меняться")
	}
}

func TestDoActionUnavailable(t *testing.T) {
	c := cardChecker(CheckerActions{}, baseUser())
	for _, code := range []string{"d", "n", "w", "B"} {
		if txt := c.doAction(context.Background(), code, 5); !strings.Contains(txt, "Недоступно") {
			t.Errorf("%s: %q", code, txt)
		}
	}
	if txt := c.doAction(context.Background(), "d", 999); !strings.Contains(txt, "не подключён") {
		t.Errorf("неизвестный: %q", txt)
	}
}

func TestBanRequiresConfirmation(t *testing.T) {
	var calls int
	act := fullActions()
	act.Ban = func(context.Context, int64) (string, error) { calls++; return "banned", nil }
	c := cardChecker(act, baseUser())
	m := c.banConfirmScreen(5, 1)
	if calls != 0 {
		t.Fatal("подтверждение не должно банить")
	}
	if !hasData(m, "ck:B:5:1") || !hasData(m, "ck:u:5:1") {
		t.Fatalf("кнопки подтверждения: %v", buttonsData(m))
	}
	if !strings.Contains(m.HTML, "когда-либо платил") {
		t.Fatalf("нет предупреждения про платящих: %s", m.HTML)
	}
}

func TestCardUnknownProviderGetsQuestionMark(t *testing.T) {
	u := baseUser()
	delete(u.ASN, "2.2.2.2")
	h := cardChecker(fullActions(), u).cardScreen(5, 0).HTML
	if !strings.Contains(h, "· Неизвестно") {
		t.Fatalf("нет метки неизвестной сети: %s", h)
	}
}

func TestCardPlainNetworkIsHome(t *testing.T) {
	u := baseUser()
	u.ASN["2.2.2.2"] = ckASN{Num: 12389, Org: "Rostelecom"}
	h := cardChecker(fullActions(), u).cardScreen(5, 0).HTML
	if !strings.Contains(h, "· Rostelecom (Другое)") {
		t.Fatalf("нет домашней метки: %s", h)
	}
}

func TestCardShowsTrafficAndDevices(t *testing.T) {
	u := baseUser()
	u.Used, u.TrafficLimit = 5<<30, 30<<30
	u.Devices = []string{"macOS (INCY)", "iPhone 15 (Happ)"}
	h := cardChecker(fullActions(), u).cardScreen(5, 0).HTML
	for _, w := range []string{"5.0 ГБ из 30.0 ГБ", "macOS (INCY), iPhone 15 (Happ)"} {
		if !strings.Contains(h, w) {
			t.Errorf("нет %q в %s", w, h)
		}
	}
}

func TestCardWithoutExtrasHasNoEmptyRows(t *testing.T) {
	h := cardChecker(fullActions(), baseUser()).cardScreen(5, 0).HTML
	if strings.Contains(h, "Трафик") || strings.Contains(h, "Устройства") {
		t.Fatalf("лишнее: %s", h)
	}
}

func TestDeviceLabel(t *testing.T) {
	cases := []struct {
		d    api.HWIDDevice
		want string
	}{
		{api.HWIDDevice{Platform: "macOS", UserAgent: "INCY/2.6.0/macos CFNetwork/1"}, "macOS (INCY)"},
		{api.HWIDDevice{Platform: "iOS", DeviceModel: "iPhone 15", UserAgent: "Happ/3.1"}, "iPhone 15 (Happ)"},
		{api.HWIDDevice{Platform: "Windows", UserAgent: "Mozilla/5.0 (Windows)"}, "Windows"},
		{api.HWIDDevice{}, "?"},
	}
	for _, c := range cases {
		if got := deviceLabel(c.d); got != c.want {
			t.Errorf("%+v: %q, нужно %q", c.d, got, c.want)
		}
	}
}

func TestBytes(t *testing.T) {
	if trafficText(0, 0) != "" || trafficText(3<<30, 0) != "3.0 ГБ" || formatBytes(512<<20) != "512 МБ" {
		t.Fatal("traffic")
	}
}

func TestFillLoadsTrafficAndDevices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "hwid") {
			_, _ = w.Write([]byte(`{"response":{"total":1,"devices":[{"platform":"macOS","userAgent":"INCY/2.6.0/macos"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"response":{"username":"vpn_u","status":"ACTIVE","trafficLimitBytes":1073741824,"userTraffic":{"usedTrafficBytes":536870912}}}`))
	}))
	defer srv.Close()
	c := &checker{api: api.NewClient(srv.URL, "t")}
	u := &ckUser{IPs: map[string]bool{}, Cities: map[string][]string{}}
	c.fill(context.Background(), nil, 1, u)
	if u.Used != 512<<20 || u.TrafficLimit != 1<<30 || len(u.Devices) != 1 || u.Devices[0] != "macOS (INCY)" {
		t.Fatalf("%+v", u)
	}
}

func TestGroupDevices(t *testing.T) {
	in := []string{"Android (Happ)", "iOS (Happ)", "iOS", "Linux", "iOS (Happ)", "iOS (v2raytun)", "Windows", "iOS (Happ)", "iOS (Happ)"}
	want := "Android (Happ), iOS ×6 (Happ, v2raytun), Linux, Windows"
	if got := groupDevices(in); got != want {
		t.Fatalf("%q, нужно %q", got, want)
	}
}

func TestShortNode(t *testing.T) {
	cases := map[string]string{
		"Norway-Server": "NR", "UK-Server": "UK", "Switzerland-Server": "SW",
		"Germany-Server": "GR", "de-01": "de-01", "-Server": "-Server",
	}
	for in, want := range cases {
		if got := shortNode(in); got != want {
			t.Errorf("%q: %q, нужно %q", in, got, want)
		}
	}
}

func TestCardNodesAreShort(t *testing.T) {
	u := baseUser()
	u.Nodes = map[string][]string{"Norway-Server": {"1.1.1.1"}, "UK-Server": {"2.2.2.2"}}
	h := cardChecker(fullActions(), u).cardScreen(5, 0).HTML
	if !strings.Contains(h, "NR (1), UK (1)") || strings.Contains(h, "Norway") {
		t.Fatalf("%s", h)
	}
}

func TestCardIPLinesShowShortNode(t *testing.T) {
	u := baseUser()
	u.IPNode = map[string]string{"1.1.1.1": "Norway-Server"}
	h := cardChecker(fullActions(), u).cardScreen(5, 0).HTML
	if !strings.Contains(h, "MTS (LTE) · NR<br>") {
		t.Fatalf("нет ноды у IP: %s", h)
	}
	if strings.Contains(h, "2.2.2.2</code> · Hetzner (Дата-центр) ·") {
		t.Fatalf("лишняя нода у IP без данных: %s", h)
	}
}
