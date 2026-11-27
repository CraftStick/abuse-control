package monitor

import (
	"context"
	"errors"
	"testing"
)

type fakeBanner struct {
	calls  int
	tgID   int64
	result string
	err    error
}

func (b *fakeBanner) Ban(_ context.Context, tg int64, _ string) (string, error) {
	b.calls++
	b.tgID = tg
	return b.result, b.err
}

func banHarness(t *testing.T, b *fakeBanner, telegramID int64) *harness {
	t.Helper()
	h := geoHarness(t, "ban", geoIPs)
	h.store.users[1].TelegramID = telegramID
	h.m.SetBanner(b)
	return h
}

func TestGeoBan_BannedDoesNotDisable(t *testing.T) {
	b := &fakeBanner{result: "banned"}
	h := banHarness(t, b, 555)
	h.checkUser(1, "10.0.1.1", "10.0.2.1", "10.0.3.1")
	if b.calls != 1 || b.tgID != 555 {
		t.Fatalf("calls=%d tg=%d", b.calls, b.tgID)
	}
	if len(h.panel.disabled) != 0 {
		t.Fatalf("после бана панель трогать не нужно: %v", h.panel.disabled)
	}
	if len(h.bot.messages) != 1 {
		t.Fatalf("messages=%d, want 1", len(h.bot.messages))
	}
}

func TestGeoBan_PaidFallsBackToDisable(t *testing.T) {
	b := &fakeBanner{result: "paid"}
	h := banHarness(t, b, 555)
	h.checkUser(1, "10.0.1.1", "10.0.2.1", "10.0.3.1")
	if len(h.panel.disabled) != 1 {
		t.Fatalf("платящего нужно хотя бы отключить: %v", h.panel.disabled)
	}
}

func TestGeoBan_ErrorFallsBackToDisable(t *testing.T) {
	b := &fakeBanner{err: errors.New("bot down")}
	h := banHarness(t, b, 555)
	h.checkUser(1, "10.0.1.1", "10.0.2.1", "10.0.3.1")
	if len(h.panel.disabled) != 1 {
		t.Fatalf("при недоступном боте должно сработать отключение: %v", h.panel.disabled)
	}
}

func TestGeoBan_UnknownOrAlreadyBannedFallsBackToDisable(t *testing.T) {
	for _, res := range []string{"unknown", "already_banned"} {
		b := &fakeBanner{result: res}
		h := banHarness(t, b, 555)
		h.checkUser(1, "10.0.1.1", "10.0.2.1", "10.0.3.1")
		if len(h.panel.disabled) != 1 {
			t.Errorf("%s: disabled=%v", res, h.panel.disabled)
		}
	}
}

func TestGeoBan_NoTelegramIDSkipsBotCall(t *testing.T) {
	b := &fakeBanner{result: "banned"}
	h := banHarness(t, b, 0)
	h.checkUser(1, "10.0.1.1", "10.0.2.1", "10.0.3.1")
	if b.calls != 0 {
		t.Fatalf("боту нечего передавать без telegram id: %d вызовов", b.calls)
	}
	if len(h.panel.disabled) != 1 {
		t.Fatalf("disabled=%v", h.panel.disabled)
	}
}

func TestGeoBan_NoBannerConfiguredDisables(t *testing.T) {
	h := geoHarness(t, "ban", geoIPs)
	h.store.users[1].TelegramID = 555
	h.checkUser(1, "10.0.1.1", "10.0.2.1", "10.0.3.1")
	if len(h.panel.disabled) != 1 {
		t.Fatalf("disabled=%v", h.panel.disabled)
	}
}
