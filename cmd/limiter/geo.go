package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/remnawave/limiter/internal/config"
	"github.com/remnawave/limiter/internal/geoip"
	"github.com/remnawave/limiter/internal/i18n"
)

const cityEdition = "GeoLite2-City"

func setupCityDB(ctx context.Context, cfg *config.Config, logger *logrus.Logger) *geoip.CityResolver {
	if !cfg.GeoBlock {
		return nil
	}
	path := cfg.GeoCityDBPath
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logger.Errorf(i18n.T("log.geo_mkdir_failed"), filepath.Dir(path), err)
		return nil
	}
	dl := &geoip.Downloader{
		Edition:    cityEdition,
		LicenseKey: cfg.MaxMindLicenseKey,
		Validate:   geoip.DefaultValidate,
	}
	needDownload, dbErr := geoip.NeedsDownload(path)
	if !needDownload {
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > cfg.MaxMindUpdateInterval {
			needDownload = true
		}
	}
	if needDownload && cfg.MaxMindLicenseKey != "" {
		if dbErr != nil {
			logger.Warnf(i18n.T("log.geo_db_invalid"), path, dbErr)
		}
		dctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		err := dl.Download(dctx, path)
		cancel()
		if err != nil {
			logger.Errorf(i18n.T("log.geo_db_download_failed"), err)
		} else {
			logger.Infof(i18n.T("log.geo_db_downloaded"), path)
		}
	}
	db, err := geoip.NewCityResolver(path)
	if err != nil {
		logger.Errorf(i18n.T("log.geo_db_open_failed"), err)
		return nil
	}
	logger.Infof(i18n.T("log.geo_enabled"), path, cfg.GeoAction, cfg.GeoMultiplier)
	if cfg.MaxMindLicenseKey != "" {
		updater := &geoip.Updater{
			Downloader: dl,
			Reloader:   db,
			DstPath:    path,
			Interval:   cfg.MaxMindUpdateInterval,
			Logger:     logger,
		}
		go updater.Run(ctx)
	}
	return db
}
