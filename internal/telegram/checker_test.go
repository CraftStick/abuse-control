package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/remnawave/limiter/internal/api"
	"github.com/remnawave/limiter/internal/cache"
)

// fill вызывается параллельно из refresh: каждая горутина владеет своим ckUser.
func TestCheckerFillParallel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"response":{"username":"vpn_u","status":"ACTIVE","hwidDeviceLimit":3}}`))
	}))
	defer srv.Close()
	c := &checker{api: api.NewClient(srv.URL, "t")}

	var wg sync.WaitGroup
	users := make([]*ckUser, 20)
	for i := range users {
		users[i] = &ckUser{IPs: map[string]bool{"bad-ip": true}, Cities: map[string][]string{}}
		wg.Add(1)
		go func() { defer wg.Done(); c.fill(context.Background(), nil, int64(i), users[i]) }()
	}
	wg.Wait()
	for _, u := range users {
		if u.Name != "vpn_u" || u.Limit != 3 || u.RU != 1 || len(u.Cities["Unknown"]) != 1 {
			t.Fatalf("неверный результат fill: %+v", u)
		}
	}
}

func TestCheckerEventsScreenOrdered(t *testing.T) {
	now := time.Now().UTC()
	c := &checker{
		events: func(context.Context, time.Duration) ([]cache.ViolationEvent, error) {
			return []cache.ViolationEvent{{At: now.Add(-time.Hour), UserID: "7", Username: "alice"}, {At: now.Add(-time.Minute), UserID: "9"}}, nil
		}}
	h := c.eventsScreen(context.Background()).HTML
	for _, w := range []string{"alice", "id 9"} {
		if !strings.Contains(h, w) {
			t.Fatalf("нет %q в %s", w, h)
		}
	}
	if strings.Index(h, "alice") > strings.Index(h, "id 9") {
		t.Fatalf("порядок по времени нарушен: %s", h)
	}
}

func TestCheckerEventsScreenEmpty(t *testing.T) {
	c := &checker{events: func(context.Context, time.Duration) ([]cache.ViolationEvent, error) { return nil, nil }}
	if h := c.eventsScreen(context.Background()).HTML; !strings.Contains(h, "нарушений не было") {
		t.Fatalf("пустой экран: %s", h)
	}
}

func TestCheckerStatusScreen(t *testing.T) {
	cases := []struct {
		name string
		last time.Time
		want string
	}{
		{"свежая проверка", time.Now().Add(-time.Minute), "работает"},
		{"давно не было", time.Now().Add(-time.Hour), "не отвечает"},
		{"ещё не было", time.Time{}, "ждёт первой проверки"},
	}
	for _, tc := range cases {
		c := &checker{health: func() CheckerHealth { return CheckerHealth{LastCheck: tc.last, Action: "ban", Mult: 4} }}
		h := c.statusScreen(context.Background()).HTML
		if !strings.Contains(h, tc.want) || !strings.Contains(h, "действие: ban") || !strings.Contains(h, "лимит × 4") {
			t.Errorf("%s: %s", tc.name, h)
		}
	}
}

func TestCheckerMultFallsBackToFour(t *testing.T) {
	c := &checker{health: func() CheckerHealth { return CheckerHealth{} }}
	if c.mult() != 4 {
		t.Fatalf("mult=%d", c.mult())
	}
	c = &checker{health: func() CheckerHealth { return CheckerHealth{Mult: 6} }}
	if c.mult() != 6 {
		t.Fatalf("mult=%d", c.mult())
	}
}

func TestEnableCheckerNeedsDatabaseAndHealth(t *testing.T) {
	b := &Bot{}
	b.EnableChecker(context.Background(), nil, "", func() CheckerHealth { return CheckerHealth{} }, CheckerActions{}, nil)
	if b.ck != nil {
		t.Fatal("без базы кнопки включаться не должны")
	}
	b.EnableChecker(context.Background(), nil, "/db", nil, CheckerActions{}, nil)
	if b.ck != nil {
		t.Fatal("без источника состояния кнопки включаться не должны")
	}
}
