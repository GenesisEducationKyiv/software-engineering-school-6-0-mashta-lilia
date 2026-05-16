package repository

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// MigrationResult tells callers what happened during RunMigrations so they
// can log appropriately. Returning a structured value (instead of swallowing
// the migrate.ErrNoChange sentinel here) keeps logging concerns out of the
// repository package.
type MigrationResult struct {
	Applied bool
}

// RunMigrations applies all pending up-migrations from sourceURL against db.
// It returns MigrationResult.Applied=false if the schema was already current.
//
// This used to live in cmd/server/main.go, but main is not the Information
// Expert here — the repository package owns Postgres concerns, including
// schema evolution.
func RunMigrations(db *sql.DB, sourceURL string) (MigrationResult, error) {
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return MigrationResult{}, fmt.Errorf("creating migration driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(sourceURL, "postgres", driver)
	if err != nil {
		return MigrationResult{}, fmt.Errorf("creating migrator: %w", err)
	}

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			return MigrationResult{Applied: false}, nil
		}
		return MigrationResult{}, fmt.Errorf("running migrations: %w", err)
	}
	return MigrationResult{Applied: true}, nil
}
