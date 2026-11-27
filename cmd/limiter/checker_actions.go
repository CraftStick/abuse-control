package main

import (
	"context"

	"github.com/remnawave/limiter/internal/api"
	"github.com/remnawave/limiter/internal/botban"
	"github.com/remnawave/limiter/internal/cache"
	"github.com/remnawave/limiter/internal/config"
	"github.com/remnawave/limiter/internal/geoip"
	"github.com/remnawave/limiter/internal/telegram"
)

func buildCheckerActions(apiClient *api.Client, c *cache.Cache, asn geoip.Resolver, cfg *config.Config) telegram.CheckerActions {
	act := telegram.CheckerActions{
		ASN: func(ip string) (uint32, string, bool) {
			info, ok := asn.Lookup(ip)
			return info.Number, info.Org, ok
		},
		Disable: apiClient.DisableUser,
		Enable:  apiClient.EnableUser,
		Whitelist: func(ctx context.Context, userID int64) error {
			return c.AddToWhitelist(ctx, userID)
		},
	}
	if cfg.BotBanURL != "" && cfg.BotBanToken != "" {
		client := botban.New(cfg.BotBanURL, cfg.BotBanToken)
		act.Ban = func(ctx context.Context, telegramID int64) (string, error) {
			return client.Ban(ctx, telegramID, "manual")
		}
	}
	return act
}
