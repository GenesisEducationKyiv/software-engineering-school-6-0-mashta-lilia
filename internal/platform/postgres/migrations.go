package postgres

import (
	"database/sql"
	"errors"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

type MigrationResult struct {
	Applied bool
}

func RunMigrations(db *sql.DB, sourceURL string) (MigrationResult, error) {
	driver, err := migratepg.WithInstance(db, &migratepg.Config{})
	if err != nil {
		return MigrationResult{}, err
	}

	m, err := migrate.NewWithDatabaseInstance(sourceURL, "postgres", driver)
	if err != nil {
		return MigrationResult{}, err
	}

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			return MigrationResult{Applied: false}, nil
		}
		return MigrationResult{}, err
	}
	return MigrationResult{Applied: true}, nil
}
