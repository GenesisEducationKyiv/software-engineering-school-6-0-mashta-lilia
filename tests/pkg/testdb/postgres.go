// Package testdb provides postgres testcontainer wiring and SQL-level
// fixtures (truncate, seed, status read-back) for integration suites.
package testdb

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

const (
	postgresStartupTimeout = 60 * time.Second
	// Wait for the 2nd "ready" line to avoid the initdb false-ready race.
	postgresReadyOccurrence = 2
)

func NewPostgres(ctx context.Context) (*sql.DB, func(), error) {
	c, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(postgresReadyOccurrence).
				WithStartupTimeout(postgresStartupTimeout),
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
		if closeErr := db.Close(); closeErr != nil {
			slog.Warn("close db on ping failure", "err", closeErr)
		}
		return nil, terminate, err
	}
	if err := runMigrations(db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			slog.Warn("close db on migrate failure", "err", closeErr)
		}
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
	// Don't mig.Close(): it would close the shared *sql.DB; we close it once at teardown.
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

func migrationsPath() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("cannot resolve testdb package directory")
	}
	return filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations"))
}
