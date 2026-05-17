package storage_test

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github-release-notifier/internal/storage"
	"github-release-notifier/internal/subscription"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	// runTests owns all setup and uses deferred cleanup so the postgres
	// container is terminated even on mid-setup failures. Calling os.Exit
	// from TestMain directly would skip any deferred Terminate and leak
	// containers across CI/dev runs.
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	flag.Parse()

	if testing.Short() {
		return m.Run()
	}

	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		slog.Error("Failed to start postgres container", "err", err)
		return 1
	}
	defer func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			slog.Warn("Failed to terminate postgres container", "err", err)
		}
	}()

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		slog.Error("Failed to get connection string", "err", err)
		return 1
	}

	testDB, err = sql.Open("postgres", connStr)
	if err != nil {
		slog.Error("Failed to open database", "err", err)
		return 1
	}
	defer func() {
		_ = testDB.Close() //nolint:errcheck // best-effort close
	}()

	if err := testDB.PingContext(ctx); err != nil {
		slog.Error("Failed to ping database", "err", err)
		return 1
	}

	driver, err := migratepg.WithInstance(testDB, &migratepg.Config{})
	if err != nil {
		slog.Error("Failed to create migration driver", "err", err)
		return 1
	}

	// Tests live under tests/storage, migrations are at the repo root.
	migrationsPath, err := filepath.Abs(filepath.Join("..", "..", "migrations"))
	if err != nil {
		slog.Error("Failed to resolve migrations path", "err", err)
		return 1
	}

	mig, err := migrate.NewWithDatabaseInstance(
		"file://"+filepath.ToSlash(migrationsPath), "postgres", driver,
	)
	if err != nil {
		slog.Error("Failed to create migrator", "err", err)
		return 1
	}
	if err := mig.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		slog.Error("Failed to run migrations", "err", err)
		return 1
	}

	return m.Run()
}

func truncateTables(t *testing.T) {
	t.Helper()
	_, err := testDB.ExecContext(context.Background(), "TRUNCATE subscriptions, tracked_repositories CASCADE")
	require.NoError(t, err)
}

// --- TrackedRepoStore Integration Tests ---

func TestIntegration_TrackedRepoStore_Upsert(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)

	store := storage.NewTrackedRepoStore(testDB)
	ctx := context.Background()

	require.NoError(t, store.Upsert(ctx, "golang", "go"))

	repos, err := store.GetAll(ctx)
	require.NoError(t, err)
	require.Len(t, repos, 1)
	repo := repos[0]
	assert.Equal(t, "golang", repo.Owner)
	assert.Equal(t, "go", repo.Name)
	assert.False(t, repo.LastSeenTag.Valid, "last_seen_tag should be NULL initially")
	assert.False(t, repo.LastCheckedAt.Valid, "last_checked_at should be NULL initially")
	assert.NotZero(t, repo.ID)

	require.NoError(t, store.Upsert(ctx, "golang", "go"))
	repos2, err := store.GetAll(ctx)
	require.NoError(t, err)
	require.Len(t, repos2, 1, "upsert should be idempotent")
	assert.Equal(t, repo.ID, repos2[0].ID, "upsert should preserve the row's ID")
}

func TestIntegration_TrackedRepoStore_UpdateLastSeen(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)

	store := storage.NewTrackedRepoStore(testDB)
	ctx := context.Background()

	require.NoError(t, store.Upsert(ctx, "golang", "go"))
	repos, err := store.GetAll(ctx)
	require.NoError(t, err)
	require.Len(t, repos, 1)
	repoID := repos[0].ID

	require.NoError(t, store.UpdateLastSeen(ctx, repoID, "v1.22.0"))

	var tag sql.NullString
	var checkedAt sql.NullTime
	err = testDB.QueryRowContext(ctx,
		"SELECT last_seen_tag, last_checked_at FROM tracked_repositories WHERE id = $1", repoID,
	).Scan(&tag, &checkedAt)
	require.NoError(t, err)
	assert.True(t, tag.Valid)
	assert.Equal(t, "v1.22.0", tag.String)
	assert.True(t, checkedAt.Valid, "last_checked_at should be set")
}

func TestIntegration_TrackedRepoStore_GetAll(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)

	store := storage.NewTrackedRepoStore(testDB)
	ctx := context.Background()

	repos, err := store.GetAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, repos)

	require.NoError(t, store.Upsert(ctx, "golang", "go"))
	require.NoError(t, store.Upsert(ctx, "rust-lang", "rust"))

	repos, err = store.GetAll(ctx)
	require.NoError(t, err)
	assert.Len(t, repos, 2)
}

// --- SubscriptionRepo Integration Tests ---

func createTrackedRepo(t *testing.T, owner, name string) {
	t.Helper()
	store := storage.NewTrackedRepoStore(testDB)
	require.NoError(t, store.Upsert(context.Background(), owner, name))
}

func TestIntegration_SubscriptionRepo_CreateAndGetByToken(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)
	createTrackedRepo(t, "golang", "go")

	repo := storage.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	sub := &subscription.Subscription{
		Email:     "user@example.com",
		RepoOwner: "golang",
		RepoName:  "go",
		Token:     "test-token-001",
		Status:    subscription.StatusPending,
	}

	err := repo.Create(ctx, sub)
	require.NoError(t, err)
	assert.NotZero(t, sub.ID, "ID should be set by RETURNING clause")
	assert.False(t, sub.CreatedAt.IsZero(), "created_at should be set")
	assert.False(t, sub.UpdatedAt.IsZero(), "updated_at should be set")

	found, err := repo.GetByToken(ctx, "test-token-001")
	require.NoError(t, err)
	assert.Equal(t, sub.ID, found.ID)
	assert.Equal(t, "user@example.com", found.Email)
	assert.Equal(t, subscription.StatusPending, found.Status)
}

func TestIntegration_SubscriptionRepo_GetByToken_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)

	repo := storage.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	_, err := repo.GetByToken(ctx, "nonexistent-token")
	require.Error(t, err)
	assert.True(t, errors.Is(err, subscription.ErrNotFound))
}

func TestIntegration_SubscriptionRepo_UpdateStatus_TriggersUpdatedAt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)
	createTrackedRepo(t, "golang", "go")

	repo := storage.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	sub := &subscription.Subscription{
		Email:     "user@example.com",
		RepoOwner: "golang",
		RepoName:  "go",
		Token:     "test-token-002",
		Status:    subscription.StatusPending,
	}
	require.NoError(t, repo.Create(ctx, sub))
	originalUpdatedAt := sub.UpdatedAt

	time.Sleep(10 * time.Millisecond)

	err := repo.UpdateStatus(ctx, sub.ID, subscription.StatusActive)
	require.NoError(t, err)

	found, err := repo.GetByToken(ctx, "test-token-002")
	require.NoError(t, err)
	assert.Equal(t, subscription.StatusActive, found.Status)
	assert.True(t, found.UpdatedAt.After(originalUpdatedAt),
		"updated_at trigger should advance the timestamp")
}

func TestIntegration_SubscriptionRepo_Exists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)
	createTrackedRepo(t, "golang", "go")

	repo := storage.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	exists, err := repo.Exists(ctx, "user@example.com", "golang", "go")
	require.NoError(t, err)
	assert.False(t, exists)

	sub := &subscription.Subscription{
		Email:     "user@example.com",
		RepoOwner: "golang",
		RepoName:  "go",
		Token:     "test-token-003",
		Status:    subscription.StatusPending,
	}
	require.NoError(t, repo.Create(ctx, sub))

	exists, err = repo.Exists(ctx, "user@example.com", "golang", "go")
	require.NoError(t, err)
	assert.True(t, exists)

	require.NoError(t, repo.UpdateStatus(ctx, sub.ID, subscription.StatusUnsubscribed))
	exists, err = repo.Exists(ctx, "user@example.com", "golang", "go")
	require.NoError(t, err)
	assert.False(t, exists, "unsubscribed rows should not count as existing")
}

func TestIntegration_SubscriptionRepo_PartialUniqueIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)
	createTrackedRepo(t, "golang", "go")

	repo := storage.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	sub1 := &subscription.Subscription{
		Email: "user@example.com", RepoOwner: "golang", RepoName: "go",
		Token: "token-a", Status: subscription.StatusPending,
	}
	require.NoError(t, repo.Create(ctx, sub1))

	sub2 := &subscription.Subscription{
		Email: "user@example.com", RepoOwner: "golang", RepoName: "go",
		Token: "token-b", Status: subscription.StatusPending,
	}
	err := repo.Create(ctx, sub2)
	require.Error(t, err, "partial unique index should prevent duplicate active/pending subscriptions")

	require.NoError(t, repo.UpdateStatus(ctx, sub1.ID, subscription.StatusUnsubscribed))

	sub3 := &subscription.Subscription{
		Email: "user@example.com", RepoOwner: "golang", RepoName: "go",
		Token: "token-c", Status: subscription.StatusPending,
	}
	err = repo.Create(ctx, sub3)
	require.NoError(t, err, "should allow re-subscription after unsubscribe")
}

func TestIntegration_SubscriptionRepo_ForeignKeyConstraint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)

	repo := storage.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	sub := &subscription.Subscription{
		Email: "user@example.com", RepoOwner: "nonexistent", RepoName: "repo",
		Token: "token-fk", Status: subscription.StatusPending,
	}
	err := repo.Create(ctx, sub)
	require.Error(t, err, "FK constraint should reject subscription without tracked repo")
}

func TestIntegration_SubscriptionRepo_GetActiveByEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)
	createTrackedRepo(t, "golang", "go")
	createTrackedRepo(t, "rust-lang", "rust")

	repo := storage.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	sub1 := &subscription.Subscription{
		Email: "user@example.com", RepoOwner: "golang", RepoName: "go",
		Token: "token-active", Status: subscription.StatusPending,
	}
	require.NoError(t, repo.Create(ctx, sub1))
	require.NoError(t, repo.UpdateStatus(ctx, sub1.ID, subscription.StatusActive))

	sub2 := &subscription.Subscription{
		Email: "user@example.com", RepoOwner: "rust-lang", RepoName: "rust",
		Token: "token-pending", Status: subscription.StatusPending,
	}
	require.NoError(t, repo.Create(ctx, sub2))

	subs, err := repo.GetActiveByEmail(ctx, "user@example.com")
	require.NoError(t, err)
	assert.Len(t, subs, 1)
	assert.Equal(t, "golang", subs[0].RepoOwner)
	assert.Equal(t, subscription.StatusActive, subs[0].Status)
}

func TestIntegration_SubscriptionRepo_GetEmailsByRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)
	createTrackedRepo(t, "golang", "go")

	repo := storage.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	for i, tc := range []struct {
		email  string
		token  string
		status subscription.Status
	}{
		{"alice@example.com", "tok-1", subscription.StatusActive},
		{"bob@example.com", "tok-2", subscription.StatusActive},
		{"charlie@example.com", "tok-3", subscription.StatusPending},
	} {
		sub := &subscription.Subscription{
			Email: tc.email, RepoOwner: "golang", RepoName: "go",
			Token: tc.token, Status: subscription.StatusPending,
		}
		require.NoError(t, repo.Create(ctx, sub), "create sub %d", i)
		if tc.status == subscription.StatusActive {
			require.NoError(t, repo.UpdateStatus(ctx, sub.ID, subscription.StatusActive))
		}
	}

	emails, err := repo.GetEmailsByRepo(ctx, "golang", "go")
	require.NoError(t, err)
	assert.Len(t, emails, 2, "only active subscribers should be returned")
	assert.Contains(t, emails, "alice@example.com")
	assert.Contains(t, emails, "bob@example.com")
}

func TestIntegration_CascadeDelete_RemovesSubscriptions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)
	createTrackedRepo(t, "golang", "go")

	subRepo := storage.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	sub := &subscription.Subscription{
		Email: "user@example.com", RepoOwner: "golang", RepoName: "go",
		Token: "tok-cascade", Status: subscription.StatusPending,
	}
	require.NoError(t, subRepo.Create(ctx, sub))

	_, err := testDB.ExecContext(
		ctx, "DELETE FROM tracked_repositories WHERE owner = $1 AND name = $2", "golang", "go",
	)
	require.NoError(t, err)

	_, err = subRepo.GetByToken(ctx, "tok-cascade")
	require.Error(t, err)
	assert.True(t, errors.Is(err, subscription.ErrNotFound), "subscription should be cascade-deleted")
}
