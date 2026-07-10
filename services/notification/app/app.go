package app

import (
	"context"
	"errors"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/services/notification/config"
)

type App struct {
	cfg    *config.Config
	logger *logger.Logger
}

func New(cfg *config.Config, l *logger.Logger) *App {
	return &App{cfg: cfg, logger: l}
}

func (a *App) Run(ctx context.Context) error {
	if a == nil {
		return errors.New("notification app: nil receiver")
	}
	if ctx == nil {
		return errors.New("notification app: nil context")
	}
	if a.cfg == nil {
		return errors.New("notification app: nil config")
	}
	if a.logger == nil {
		return errors.New("notification app: nil logger")
	}

	if a.cfg.DBSSLMode == "disable" {
		a.logger.Warn(ctx, "db_sslmode_disable", "risk", "postgres credentials and pii travel in cleartext")
	}

	db, err := openAndMigrateDB(ctx, a.cfg, a.logger)
	if err != nil {
		return err
	}
	defer closeQuietly(ctx, a.logger, "database", db.Close)

	deps, err := buildDependencies(ctx, a.cfg, db, a.logger)
	if err != nil {
		return err
	}
	defer closeQuietly(ctx, a.logger, "dependencies", deps.Close)

	return runGRPCServer(ctx, a.cfg, deps, a.logger)
}

func closeQuietly(ctx context.Context, log *logger.Logger, name string, closer func() error) {
	if err := closer(); err != nil {
		log.Error(ctx, "close_resource_failed", "resource", name, "err", err)
	}
}
