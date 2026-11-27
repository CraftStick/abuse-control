package monitor

import (
	"context"
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/remnawave/limiter/internal/api"
	"github.com/remnawave/limiter/internal/config"
	"github.com/remnawave/limiter/internal/i18n"
)

type userBanner interface {
	Ban(ctx context.Context, telegramID int64, reason string) (string, error)
}

func (m *Monitor) SetBanner(b userBanner) {
	m.banner = b
}

type cityLookup interface {
	CityInCountry(ip, country string) (string, bool)
}

func (m *Monitor) SetCityResolver(c cityLookup) {
	m.cities = c
}

const geoCountry = "RU"

func (m *Monitor) geoCities(uniqueMap map[string]api.ActiveIP) []string {
	seen := make(map[string]struct{})
	for ip := range uniqueMap {
		if city, ok := m.cities.CityInCountry(ip, geoCountry); ok {
			seen[city] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func (m *Monitor) checkGeo(ctx context.Context, cfg *config.Config, user *api.CachedUser, limit int, uniqueMap map[string]api.ActiveIP) bool {
	if !cfg.GeoBlock || m.cities == nil {
		return false
	}
	threshold := limit * cfg.GeoMultiplier
	if len(uniqueMap) <= threshold {
		return false
	}
	cities := m.geoCities(uniqueMap)
	if len(cities) <= threshold {
		return false
	}

	fields := logrus.Fields{"userID": user.UserID, "cities": len(cities), "limit": limit}
	active, err := m.cache.IsCooldownActive(ctx, user.UserID)
	if err != nil {
		m.logger.WithError(err).WithFields(fields).Error(i18n.T("log.geo_cooldown_check_failed"))
		return true
	}
	if active {
		return true
	}
	if err := m.cache.SetCooldown(ctx, user.UserID, time.Duration(cfg.Cooldown)*time.Second); err != nil {
		m.logger.WithError(err).WithFields(fields).Error(i18n.T("log.geo_cooldown_set_failed"))
		return true
	}
	if err := m.cache.RecordViolation(ctx, user.UserID, user.Username); err != nil {
		m.logger.WithError(err).WithFields(fields).Warn(i18n.T("log.geo_record_failed"))
	}
	m.logger.WithFields(fields).Warn(i18n.T("log.geo_too_many_cities"))

	text := geoAlertText(user, limit, len(uniqueMap), cities, cfg.GeoMultiplier, cfg.GeoAction != "manual")
	if cfg.GeoAction == "manual" {
		if err := m.bot.SendManualAlert(ctx, text, user.UserID, cfg.AutoDisableDuration, cfg.IgnoreDuration); err != nil {
			m.logger.WithError(err).WithFields(fields).Error(i18n.T("log.geo_manual_alert_failed"))
		}
		return true
	}

	if cfg.GeoAction == "ban" {
		if m.banHuman(ctx, cfg, user, fields, cities, limit, len(uniqueMap)) {
			return true
		}
		text = geoAlertText(user, limit, len(uniqueMap), cities, cfg.GeoMultiplier, true)
	}
	m.geoDisable(ctx, cfg, user, fields, text)
	return true
}

func (m *Monitor) banHuman(ctx context.Context, cfg *config.Config, user *api.CachedUser, fields logrus.Fields, cities []string, limit, sessions int) bool {
	if m.banner == nil || user.TelegramID <= 0 {
		m.logger.WithFields(fields).Warn(i18n.T("log.geo_ban_unavailable"))
		return false
	}
	result, err := m.banner.Ban(ctx, user.TelegramID, "geo")
	if err != nil {
		m.logger.WithError(err).WithFields(fields).Error(i18n.T("log.geo_ban_request_failed"))
		return false
	}
	switch result {
	case "banned":
		m.logger.WithFields(fields).Warn(i18n.T("log.geo_banned"))
		text := geoBanText(user, limit, sessions, cities, cfg.GeoMultiplier)
		if err := m.bot.SendMessage(ctx, text); err != nil {
			m.logger.WithError(err).WithFields(fields).Error(i18n.T("log.geo_ban_alert_failed"))
		}
		return true
	case "paid":
		_ = m.bot.SendMessage(ctx, fmt.Sprintf(i18n.T("geo.paid_note"), user.UserID))
	default:
		m.logger.WithFields(fields).Warnf(i18n.T("log.geo_bot_answer"), result)
	}
	return false
}

func (m *Monitor) geoDisable(ctx context.Context, cfg *config.Config, user *api.CachedUser, fields logrus.Fields, text string) {
	err := m.api.DisableUser(ctx, user.UserID)
	if api.IsAlreadyDisabled(err) {
		m.extendCooldown(ctx, cfg, user.UserID)
		return
	}
	if err != nil {
		m.logger.WithError(err).WithFields(fields).Error(i18n.T("log.geo_disable_failed"))
		if ctx.Err() == nil {
			_ = m.bot.SendMessage(ctx, fmt.Sprintf(i18n.T("geo.disable_failed"), user.UserID, html.EscapeString(err.Error())))
		}
		return
	}
	m.extendCooldown(ctx, cfg, user.UserID)
	if cfg.AutoDisableDuration > 0 {
		storeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), restoreQueueTimeout)
		defer cancel()
		if err := m.cache.SetRestoreTimer(storeCtx, user.UserID, time.Duration(cfg.AutoDisableDuration)*time.Minute); err != nil {
			m.logger.WithError(err).WithFields(fields).Error(i18n.T("log.geo_restore_timer_failed"))
		}
	}
	if err := m.bot.SendMessage(ctx, text); err != nil {
		m.logger.WithError(err).WithFields(fields).Error(i18n.T("log.geo_alert_failed"))
	}
}

func geoBanText(user *api.CachedUser, limit, sessions int, cities []string, mult int) string {
	return geoText(i18n.T("geo.head_banned"), user, limit, sessions, cities, mult)
}

func geoText(head string, user *api.CachedUser, limit, sessions int, cities []string, mult int) string {
	return "<b>" + head + "</b>\n" + fmt.Sprintf(i18n.T("geo.body"),
		user.UserID, user.TelegramID, html.EscapeString(user.Username), limit, limit*mult, sessions, len(cities),
		html.EscapeString(strings.Join(cities, "\n")))
}

func geoAlertText(user *api.CachedUser, limit, sessions int, cities []string, mult int, disabled bool) string {
	head := i18n.T("geo.head_manual")
	if disabled {
		head = i18n.T("geo.head_disabled")
	}
	return geoText(head, user, limit, sessions, cities, mult)
}
