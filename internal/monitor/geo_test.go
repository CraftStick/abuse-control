package monitor

import (
	"testing"
	"time"

	"github.com/remnawave/limiter/internal/config"
)

type fakeCities map[string]string

func (f fakeCities) CityInCountry(ip, country string) (string, bool) {
	c, ok := f[ip]
	return c, ok && country == "RU"
}

var geoIPs = fakeCities{"10.0.1.1": "Moscow", "10.0.2.1": "Kazan", "10.0.3.1": "Omsk", "10.0.4.1": "Tula"}

func geoHarness(t *testing.T, action string, cities fakeCities) *harness {
	t.Helper()
	h := newHarness(t, func(c *config.Config) {
		c.GeoBlock = true
		c.GeoAction = action
		c.GeoMultiplier = 1
		c.Cooldown = 300
	})
	h.cacheUser(1, 2)
	h.m.SetCityResolver(cities)
	return h
}

func TestGeo_AutoDisablesWhenCitiesExceedThreshold(t *testing.T) {
	h := geoHarness(t, "auto", geoIPs)
	h.checkUser(1, "10.0.1.1", "10.0.2.1", "10.0.3.1")
	if len(h.panel.disabled) != 1 {
		t.Fatalf("disabled = %v, want one user", h.panel.disabled)
	}
	if len(h.bot.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(h.bot.messages))
	}
	if h.store.called("SetCooldown") == 0 || h.store.called("RecordViolation") != 1 {
		t.Errorf("cooldown/violation not recorded")
	}
}

func TestGeo_ManualSendsAlertWithoutDisabling(t *testing.T) {
	h := geoHarness(t, "manual", geoIPs)
	h.checkUser(1, "10.0.1.1", "10.0.2.1", "10.0.3.1")
	if len(h.panel.disabled) != 0 {
		t.Fatalf("manual must not disable: %v", h.panel.disabled)
	}
	if h.bot.alerts() != 1 {
		t.Fatalf("alerts = %d, want 1", h.bot.alerts())
	}
}

func TestGeo_AtThresholdDoesNothing(t *testing.T) {
	h := geoHarness(t, "auto", geoIPs)
	h.checkUser(1, "10.0.1.1", "10.0.2.1")
	if len(h.panel.disabled) != 0 || h.bot.alerts() != 0 || len(h.bot.messages) != 0 {
		t.Fatalf("nothing expected at threshold")
	}
}

func TestGeo_SameCityManyIPsIsNotAViolation(t *testing.T) {
	same := fakeCities{"10.0.1.1": "Moscow", "10.0.1.2": "Moscow", "10.0.1.3": "Moscow", "10.0.1.4": "Moscow"}
	h := geoHarness(t, "auto", same)
	h.checkUser(1, "10.0.1.1", "10.0.1.2", "10.0.1.3", "10.0.1.4")
	if len(h.panel.disabled) != 0 {
		t.Fatalf("one city must not disable: %v", h.panel.disabled)
	}
}

func TestGeo_NonRussianIPsAreIgnored(t *testing.T) {
	h := geoHarness(t, "auto", fakeCities{})
	h.checkUser(1, "10.0.1.1", "10.0.2.1", "10.0.3.1", "10.0.4.1")
	if len(h.panel.disabled) != 0 {
		t.Fatalf("unresolved IPs must not disable: %v", h.panel.disabled)
	}
}

func TestGeo_CooldownSuppressesRepeat(t *testing.T) {
	h := geoHarness(t, "auto", geoIPs)
	h.store.cooldown[1] = time.Minute
	h.checkUser(1, "10.0.1.1", "10.0.2.1", "10.0.3.1")
	if len(h.panel.disabled) != 0 || len(h.bot.messages) != 0 {
		t.Fatalf("cooldown must suppress")
	}
}

func TestGeo_DisabledFlagIsOff(t *testing.T) {
	h := geoHarness(t, "auto", geoIPs)
	h.provider.Store(func() *config.Config { c := *h.provider.Load(); c.GeoBlock = false; return &c }())
	h.checkUser(1, "10.0.1.1", "10.0.2.1", "10.0.3.1")
	if len(h.panel.disabled) != 0 {
		t.Fatalf("GEO_BLOCK=false must not act")
	}
}

func TestGeo_AlreadyDisabledKeepsTimerAndStaysQuiet(t *testing.T) {
	h := geoHarness(t, "auto", geoIPs)
	h.store.timers[1] = 10 * time.Minute
	h.panel.failOn("DisableUser", alreadyDisabledErr())
	h.checkUser(1, "10.0.1.1", "10.0.2.1", "10.0.3.1")
	if len(h.bot.messages) != 0 || h.bot.alerts() != 0 {
		t.Errorf("уже отключённого не нужно рекламировать повторно: %d сообщений", len(h.bot.messages))
	}
	if h.store.called("SetRestoreTimer") != 0 {
		t.Error("у отключённого админом таймер восстановления ставить нельзя: подписка включилась бы сама")
	}
	if got := h.store.timers[1]; got != 10*time.Minute {
		t.Errorf("существующий таймер = %v, ожидалось 10m", got)
	}
}
