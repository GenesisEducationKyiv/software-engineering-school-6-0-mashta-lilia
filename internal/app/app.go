package app

import (
	"context"
	"errors"
	"github-release-notifier/internal/config"
	"github-release-notifier/internal/platform/logger"
	"log/slog"
)

type App struct {
	cfg    *config.Config
	logger logger.Logger
}

func New(cfg *config.Config, l logger.Logger) *App {
	return &App{cfg: cfg, logger: l}
}

func (a *App) Run(ctx context.Context) error {
	if a == nil {
		return errors.New("app: nil receiver")
	}
	if ctx == nil {
		return errors.New("app: nil context")
	}
	if a.cfg == nil {
		return errors.New("app: nil config")
	}
	if a.logger == nil {
		return errors.New("app: nil logger")
	}

	logger.SetDefault(a.logger)

	if a.cfg.DBSSLMode == "disable" {
		slog.Warn("DB_SSLMODE=disable — Postgres credentials and PII " +
			"will travel in cleartext; set to require/verify-full in production")
	}

	db, err := openAndMigrateDB(ctx, a.cfg)
	if err != nil {
		return err
	}
	defer closeQuietly("database", db.Close)

	rdb := newRedisClient(a.cfg)
	defer closeQuietly("redis", rdb.Close)

	deps, err := buildDependencies(ctx, a.cfg, db, rdb, a.logger)
	if err != nil {
		return err
	}
	defer closeQuietly("dependencies", deps.Close)

	return runHTTPServer(ctx, a.cfg, deps)
}

func closeQuietly(name string, closer func() error) {
	if err := closer(); err != nil {
		slog.Error("Failed to close resource", "resource", name, "err", err)
	}
}
