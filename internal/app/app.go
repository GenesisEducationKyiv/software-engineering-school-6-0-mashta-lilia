package app

import (
	"context"
	"github-release-notifier/internal/config"
	"log/slog"
)

type App struct {
	cfg    *config.Config
	logger *slog.Logger
}

func New(cfg *config.Config, l *slog.Logger) *App {
	return &App{cfg: cfg, logger: l}
}

func (a *App) Run(ctx context.Context) error {
	// Install the injected logger as slog default so package-level slog.*
	// calls in subscription/, release/, etc. honor the configured level.
	slog.SetDefault(a.logger)

	db, err := openAndMigrateDB(a.cfg)
	if err != nil {
		return err
	}
	defer closeQuietly("database", db.Close)

	rdb := newRedisClient(a.cfg)
	defer closeQuietly("redis", rdb.Close)

	deps, err := buildDependencies(ctx, a.cfg, db, rdb)
	if err != nil {
		return err
	}

	return runHTTPServer(ctx, a.cfg, deps)
}

func closeQuietly(name string, closer func() error) {
	if err := closer(); err != nil {
		slog.Error("Failed to close resource", "resource", name, "err", err)
	}
}
