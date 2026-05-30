package app

import (
	"context"
	"database/sql"
	"fmt"
	"github-release-notifier/internal/config"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/platform/postgres"
)

func openAndMigrateDB(ctx context.Context, cfg *config.Config, log logger.Logger) (*sql.DB, error) {
	// Migrate via a short-lived pool first so failures happen before wiring the long-lived one.
	res, err := postgres.RunMigrationsWithContext(ctx, cfg.DatabaseURL(), "file://migrations")
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

	// Re-ping under the caller ctx so SIGINT cancels post-open pool tuning too.
	if err := db.PingContext(ctx); err != nil {
		closeQuietly(ctx, log, "database", db.Close)
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return db, nil
}
