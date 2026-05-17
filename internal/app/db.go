package app

import (
	"context"
	"database/sql"
	"fmt"
	"github-release-notifier/internal/config"
	"github-release-notifier/internal/platform/postgres"
	"log/slog"
)

func openAndMigrateDB(ctx context.Context, cfg *config.Config) (*sql.DB, error) {
	// Run migrations against a dedicated short-lived pool first, so any
	// failure happens before we wire up the application's long-lived pool.
	res, err := postgres.RunMigrations(cfg.DatabaseURL(), "file://migrations")
	if err != nil {
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	if res.Applied {
		slog.Info("Migrations applied successfully")
	} else {
		slog.Info("No pending migrations")
	}

	db, err := postgres.New(cfg.DatabaseURL())
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Re-ping under the caller's ctx so a SIGINT during startup is honored
	// (postgres.New's internal ping uses its own short timeout, not our ctx).
	if err := db.PingContext(ctx); err != nil {
		closeQuietly("database", db.Close)
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return db, nil
}
