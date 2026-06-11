package app

import (
	"context"
	"database/sql"
	"fmt"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/platform/postgres"
	"github-release-notifier/services/notification/config"
)

const migrationsPath = "file://services/notification/migrations"

func openAndMigrateDB(ctx context.Context, cfg *config.Config, log *logger.Logger) (*sql.DB, error) {
	res, err := postgres.RunMigrationsWithContext(ctx, cfg.DatabaseURL(), migrationsPath)
	if err != nil {
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	if res.Applied {
		log.Info(ctx, "migrations_applied")
	} else {
		log.Info(ctx, "migrations_noop")
	}

	db, err := postgres.NewWithContext(ctx, cfg.DatabaseURL())
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		closeQuietly(ctx, log, "database", db.Close)
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return db, nil
}
