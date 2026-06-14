package store_test

import (
	"context"
	"database/sql"
	"flag"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/services/notification/store"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	platformpostgres "github-release-notifier/internal/platform/postgres"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	testpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	postgresStartupTimeout  = 60 * time.Second
	postgresReadyOccurrence = 2
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}

	db, cleanup, err := newPostgres(context.Background())
	defer cleanup()
	if err != nil {
		slog.Error("postgres setup failed", "err", err)
		return 1
	}
	testDB = db
	return m.Run()
}

func TestIntegration_Store_ReserveIsIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateNotifications(t)

	store := store.New(testDB, logger.Nop())
	ctx := context.Background()

	reserved, err := store.Reserve(ctx, "release", "release:golang/go:v1:alice@example.com")
	require.NoError(t, err)
	assert.True(t, reserved)

	reserved, err = store.Reserve(ctx, "release", "release:golang/go:v1:alice@example.com")
	require.NoError(t, err)
	assert.False(t, reserved)

	var count int
	err = testDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM sent_notifications").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestIntegration_Store_ReservePersistsMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateNotifications(t)

	store := store.New(testDB, logger.Nop())
	ctx := context.Background()

	reserved, err := store.Reserve(ctx, "confirmation", "confirm:tok")
	require.NoError(t, err)
	require.True(t, reserved)

	var kind, dedupKey string
	err = testDB.QueryRowContext(
		ctx,
		"SELECT kind, dedup_key FROM sent_notifications",
	).Scan(&kind, &dedupKey)
	require.NoError(t, err)
	assert.Equal(t, "confirmation", kind)
	assert.Equal(t, "confirm:tok", dedupKey)
}

func truncateNotifications(t *testing.T) {
	t.Helper()
	_, err := testDB.Exec("TRUNCATE sent_notifications RESTART IDENTITY")
	require.NoError(t, err)
}

func newPostgres(ctx context.Context) (*sql.DB, func(), error) {
	container, err := testpostgres.Run(
		ctx,
		"postgres:16-alpine",
		testpostgres.WithDatabase("testdb"),
		testpostgres.WithUsername("testuser"),
		testpostgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(postgresReadyOccurrence).
				WithStartupTimeout(postgresStartupTimeout),
		),
	)
	if err != nil {
		return nil, func() {}, err
	}
	terminate := func() {
		if err := container.Terminate(context.Background()); err != nil {
			slog.Warn("terminate postgres", "err", err)
		}
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, terminate, err
	}
	if _, err := platformpostgres.RunMigrationsWithContext(ctx, connStr, notificationMigrationsURL()); err != nil {
		return nil, terminate, err
	}
	db, err := platformpostgres.NewWithContext(ctx, connStr)
	if err != nil {
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

func notificationMigrationsURL() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot resolve notification store test path")
	}
	path, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "migrations"))
	if err != nil {
		panic(err)
	}
	return "file://" + filepath.ToSlash(path)
}
