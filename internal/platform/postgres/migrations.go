package postgres

import (
	"context"
	"errors"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

type MigrationResult struct {
	Applied bool
}

// Uses its own pool so m.Close() can't tear down the app's *sql.DB.
func RunMigrations(databaseURL, sourceURL string) (MigrationResult, error) {
	return RunMigrationsWithContext(context.Background(), databaseURL, sourceURL)
}

func RunMigrationsWithContext(
	ctx context.Context, databaseURL, sourceURL string,
) (result MigrationResult, retErr error) {
	if ctx == nil {
		return MigrationResult{}, errors.New("postgres migrations: nil context")
	}

	m, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		return MigrationResult{}, err
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if closeErr := errors.Join(srcErr, dbErr); closeErr != nil {
			retErr = errors.Join(retErr, closeErr)
		}
	}()

	if err := ctx.Err(); err != nil {
		return MigrationResult{}, err
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			select {
			case m.GracefulStop <- true:
			case <-done:
			}
		case <-done:
		}
	}()

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			return MigrationResult{Applied: false}, nil
		}
		return MigrationResult{}, err
	}
	return MigrationResult{Applied: true}, nil
}
