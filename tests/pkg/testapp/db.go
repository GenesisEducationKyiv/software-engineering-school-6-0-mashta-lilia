package testapp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// NewPostgres starts a postgres testcontainer, opens a *sql.DB against it,
// and applies the repo's migrations. Returns the DB and a cleanup func that
// closes the connection and terminates the container.
func NewPostgres(ctx context.Context) (*sql.DB, func(), error) {
	c, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return nil, func() {}, fmt.Errorf("postgres: %w", err)
	}
	terminate := func() {
		if err := c.Terminate(context.Background()); err != nil {
			slog.Warn("terminate postgres", "err", err)
		}
	}

	connStr, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, terminate, err
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, terminate, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, terminate, err
	}
	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, terminate, err
	}

	cleanup := func() {
		if err := db.Close(); err != nil {
			slog.Warn("close test db", "err", err)
		}
		terminate()
	}
	return db, cleanup, nil
}

func runMigrations(db *sql.DB) error {
	driver, err := migratepg.WithInstance(db, &migratepg.Config{})
	if err != nil {
		return fmt.Errorf("migrate driver: %w", err)
	}
	path, err := migrationsPath()
	if err != nil {
		return err
	}
	// Deliberately not calling mig.Close(): golang-migrate's Close() closes
	// both the source AND the database driver, which wraps the *sql.DB we
	// share with the rest of the test suite. The file-source has no real
	// resources to release; the db handle is closed once at suite teardown.
	mig, err := migrate.NewWithDatabaseInstance(
		"file://"+filepath.ToSlash(path), "postgres", driver,
	)
	if err != nil {
		return fmt.Errorf("migrate new: %w", err)
	}
	if err := mig.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// migrationsPath resolves the absolute path of the repo's `migrations`
// directory relative to this source file so callers don't need to know
// where in the tree they're running from.
func migrationsPath() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("cannot resolve testapp package directory")
	}
	// file = .../tests/pkg/testapp/db.go -> repo root is three levels up.
	return filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations"))
}
