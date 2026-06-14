package postgres

import (
	"context"
	"errors"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4/source/iofs"
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
) (MigrationResult, error) {
	if ctx == nil {
		return MigrationResult{}, errors.New("postgres migrations: nil context")
	}
	m, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		return MigrationResult{}, err
	}
	return runMigrations(ctx, m)
}

// RunMigrationsFSWithContext runs migrations from an embedded filesystem so the
// source is deployment-stable regardless of the process working directory.
func RunMigrationsFSWithContext(
	ctx context.Context, databaseURL string, fsys fs.FS, path string,
) (MigrationResult, error) {
	if ctx == nil {
		return MigrationResult{}, errors.New("postgres migrations: nil context")
	}
	src, err := iofs.New(fsys, path)
	if err != nil {
		return MigrationResult{}, err
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, databaseURL)
	if err != nil {
		return MigrationResult{}, err
	}
	return runMigrations(ctx, m)
}

func runMigrations(ctx context.Context, m *migrate.Migrate) (result MigrationResult, retErr error) {
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
