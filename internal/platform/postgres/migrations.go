package postgres

import (
	"errors"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

type MigrationResult struct {
	Applied bool
}

// RunMigrations opens its own short-lived connection pool via migrate.New
// rather than sharing the application's *sql.DB. This keeps lifecycle clean:
// m.Close() shuts down its own pool without affecting the app's, and no
// migrator-held connection lingers for the life of the process.
func RunMigrations(databaseURL, sourceURL string) (MigrationResult, error) {
	m, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		return MigrationResult{}, err
	}
	defer func() { _, _ = m.Close() }() //nolint:errcheck // best-effort close; pool is short-lived

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			return MigrationResult{Applied: false}, nil
		}
		return MigrationResult{}, err
	}
	return MigrationResult{Applied: true}, nil
}
