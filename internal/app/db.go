package app

import (
	"database/sql"
	"fmt"
	"github-release-notifier/internal/config"
	"github-release-notifier/internal/platform/postgres"
	"log/slog"
)

func openAndMigrateDB(cfg *config.Config) (*sql.DB, error) {
	db, err := postgres.New(cfg.DatabaseURL())
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	res, err := postgres.RunMigrations(db, "file://migrations")
	if err != nil {
		closeQuietly("database", db.Close)
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	if res.Applied {
		slog.Info("Migrations applied successfully")
	} else {
		slog.Info("No pending migrations")
	}
	return db, nil
}
