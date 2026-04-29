package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github-release-notifier/internal/model"
	"github-release-notifier/internal/repository"

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
	// Parse flags so testing.Short() is available in TestMain
	flag.Parse()

	if testing.Short() {
		os.Exit(m.Run())
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
		log.Fatalf("failed to start postgres container: %v", err)
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("failed to get connection string: %v", err)
	}

	testDB, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}

	if err := testDB.Ping(); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}

	// Run real migrations
	driver, err := migratepg.WithInstance(testDB, &migratepg.Config{})
	if err != nil {
		log.Fatalf("failed to create migration driver: %v", err)
	}

	migrationsPath, err := filepath.Abs(filepath.Join("..", "..", "migrations"))
	if err != nil {
		log.Fatalf("failed to resolve migrations path: %v", err)
	}

	mig, err := migrate.NewWithDatabaseInstance("file://"+filepath.ToSlash(migrationsPath), "postgres", driver)
	if err != nil {
		log.Fatalf("failed to create migrator: %v", err)
	}
	if err := mig.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("failed to run migrations: %v", err)
	}

	code := m.Run()

	testDB.Close()
	if err := pgContainer.Terminate(ctx); err != nil {
		log.Printf("failed to terminate postgres container: %v", err)
	}

	os.Exit(code)
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

	store := repository.NewTrackedRepoStore(testDB)
	ctx := context.Background()

	// First upsert creates the row
	repo, err := store.Upsert(ctx, "golang", "go")
	require.NoError(t, err)
	assert.Equal(t, "golang", repo.Owner)
	assert.Equal(t, "go", repo.Name)
	assert.False(t, repo.LastSeenTag.Valid, "last_seen_tag should be NULL initially")
	assert.False(t, repo.LastCheckedAt.Valid, "last_checked_at should be NULL initially")
	assert.NotZero(t, repo.ID)

	// Second upsert is idempotent — returns same row
	repo2, err := store.Upsert(ctx, "golang", "go")
	require.NoError(t, err)
	assert.Equal(t, repo.ID, repo2.ID, "upsert should return same ID")
}

func TestIntegration_TrackedRepoStore_UpdateLastSeen(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)

	store := repository.NewTrackedRepoStore(testDB)
	ctx := context.Background()

	repo, err := store.Upsert(ctx, "golang", "go")
	require.NoError(t, err)

	err = store.UpdateLastSeen(ctx, repo.ID, "v1.22.0")
	require.NoError(t, err)

	// Verify directly in the DB
	var tag sql.NullString
	var checkedAt sql.NullTime
	err = testDB.QueryRowContext(ctx,
		"SELECT last_seen_tag, last_checked_at FROM tracked_repositories WHERE id = $1", repo.ID,
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

	store := repository.NewTrackedRepoStore(testDB)
	ctx := context.Background()

	// Empty initially
	repos, err := store.GetAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, repos)

	// Add two repos
	_, err = store.Upsert(ctx, "golang", "go")
	require.NoError(t, err)
	_, err = store.Upsert(ctx, "rust-lang", "rust")
	require.NoError(t, err)

	repos, err = store.GetAll(ctx)
	require.NoError(t, err)
	assert.Len(t, repos, 2)
}

// --- SubscriptionRepo Integration Tests ---

func createTrackedRepo(t *testing.T, owner, name string) {
	t.Helper()
	store := repository.NewTrackedRepoStore(testDB)
	_, err := store.Upsert(context.Background(), owner, name)
	require.NoError(t, err)
}

func TestIntegration_SubscriptionRepo_CreateAndGetByToken(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)
	createTrackedRepo(t, "golang", "go")

	repo := repository.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	sub := &model.Subscription{
		Email:     "user@example.com",
		RepoOwner: "golang",
		RepoName:  "go",
		Token:     "test-token-001",
		Status:    model.StatusPending,
	}

	err := repo.Create(ctx, sub)
	require.NoError(t, err)
	assert.NotZero(t, sub.ID, "ID should be set by RETURNING clause")
	assert.False(t, sub.CreatedAt.IsZero(), "created_at should be set")
	assert.False(t, sub.UpdatedAt.IsZero(), "updated_at should be set")

	// Retrieve by token
	found, err := repo.GetByToken(ctx, "test-token-001")
	require.NoError(t, err)
	assert.Equal(t, sub.ID, found.ID)
	assert.Equal(t, "user@example.com", found.Email)
	assert.Equal(t, model.StatusPending, found.Status)
}

func TestIntegration_SubscriptionRepo_GetByToken_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)

	repo := repository.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	_, err := repo.GetByToken(ctx, "nonexistent-token")
	require.Error(t, err)
	assert.True(t, errors.Is(err, repository.ErrNotFound))
}

func TestIntegration_SubscriptionRepo_UpdateStatus_TriggersUpdatedAt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)
	createTrackedRepo(t, "golang", "go")

	repo := repository.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	sub := &model.Subscription{
		Email:     "user@example.com",
		RepoOwner: "golang",
		RepoName:  "go",
		Token:     "test-token-002",
		Status:    model.StatusPending,
	}
	require.NoError(t, repo.Create(ctx, sub))
	originalUpdatedAt := sub.UpdatedAt

	// Small delay to ensure updated_at differs
	time.Sleep(10 * time.Millisecond)

	err := repo.UpdateStatus(ctx, sub.ID, model.StatusActive)
	require.NoError(t, err)

	// Verify the trigger updated updated_at
	found, err := repo.GetByToken(ctx, "test-token-002")
	require.NoError(t, err)
	assert.Equal(t, model.StatusActive, found.Status)
	assert.True(t, found.UpdatedAt.After(originalUpdatedAt), "updated_at trigger should advance the timestamp")
}

func TestIntegration_SubscriptionRepo_Exists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)
	createTrackedRepo(t, "golang", "go")

	repo := repository.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	// Does not exist yet
	exists, err := repo.Exists(ctx, "user@example.com", "golang", "go")
	require.NoError(t, err)
	assert.False(t, exists)

	// Create pending subscription
	sub := &model.Subscription{
		Email:     "user@example.com",
		RepoOwner: "golang",
		RepoName:  "go",
		Token:     "test-token-003",
		Status:    model.StatusPending,
	}
	require.NoError(t, repo.Create(ctx, sub))

	// Now exists (pending counts)
	exists, err = repo.Exists(ctx, "user@example.com", "golang", "go")
	require.NoError(t, err)
	assert.True(t, exists)

	// Unsubscribe — should no longer "exist" (freed slot)
	require.NoError(t, repo.UpdateStatus(ctx, sub.ID, model.StatusUnsubscribed))
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

	repo := repository.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	// Create first subscription
	sub1 := &model.Subscription{
		Email: "user@example.com", RepoOwner: "golang", RepoName: "go",
		Token: "token-a", Status: model.StatusPending,
	}
	require.NoError(t, repo.Create(ctx, sub1))

	// Duplicate should be rejected by partial unique index
	sub2 := &model.Subscription{
		Email: "user@example.com", RepoOwner: "golang", RepoName: "go",
		Token: "token-b", Status: model.StatusPending,
	}
	err := repo.Create(ctx, sub2)
	require.Error(t, err, "partial unique index should prevent duplicate active/pending subscriptions")

	// Unsubscribe first, then retry — should succeed (index only covers non-unsubscribed)
	require.NoError(t, repo.UpdateStatus(ctx, sub1.ID, model.StatusUnsubscribed))

	sub3 := &model.Subscription{
		Email: "user@example.com", RepoOwner: "golang", RepoName: "go",
		Token: "token-c", Status: model.StatusPending,
	}
	err = repo.Create(ctx, sub3)
	require.NoError(t, err, "should allow re-subscription after unsubscribe")
}

func TestIntegration_SubscriptionRepo_ForeignKeyConstraint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)

	// Do NOT create tracked repo — FK should fail
	repo := repository.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	sub := &model.Subscription{
		Email: "user@example.com", RepoOwner: "nonexistent", RepoName: "repo",
		Token: "token-fk", Status: model.StatusPending,
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

	repo := repository.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	// Create active subscription
	sub1 := &model.Subscription{
		Email: "user@example.com", RepoOwner: "golang", RepoName: "go",
		Token: "token-active", Status: model.StatusPending,
	}
	require.NoError(t, repo.Create(ctx, sub1))
	require.NoError(t, repo.UpdateStatus(ctx, sub1.ID, model.StatusActive))

	// Create pending subscription (should NOT appear)
	sub2 := &model.Subscription{
		Email: "user@example.com", RepoOwner: "rust-lang", RepoName: "rust",
		Token: "token-pending", Status: model.StatusPending,
	}
	require.NoError(t, repo.Create(ctx, sub2))

	// Only active should be returned
	subs, err := repo.GetActiveByEmail(ctx, "user@example.com")
	require.NoError(t, err)
	assert.Len(t, subs, 1)
	assert.Equal(t, "golang", subs[0].RepoOwner)
	assert.Equal(t, model.StatusActive, subs[0].Status)
}

func TestIntegration_SubscriptionRepo_GetEmailsByRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)
	createTrackedRepo(t, "golang", "go")

	repo := repository.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	// Two active subscribers, one pending
	for i, tc := range []struct {
		email  string
		token  string
		status model.SubscriptionStatus
	}{
		{"alice@example.com", "tok-1", model.StatusActive},
		{"bob@example.com", "tok-2", model.StatusActive},
		{"charlie@example.com", "tok-3", model.StatusPending},
	} {
		sub := &model.Subscription{
			Email: tc.email, RepoOwner: "golang", RepoName: "go",
			Token: tc.token, Status: model.StatusPending,
		}
		require.NoError(t, repo.Create(ctx, sub), "create sub %d", i)
		if tc.status == model.StatusActive {
			require.NoError(t, repo.UpdateStatus(ctx, sub.ID, model.StatusActive))
		}
	}

	emails, err := repo.GetEmailsByRepo(ctx, "golang", "go")
	require.NoError(t, err)
	assert.Len(t, emails, 2, "only active subscribers should be returned")
	assert.Contains(t, emails, "alice@example.com")
	assert.Contains(t, emails, "bob@example.com")
}

// --- FK Cascade Integration Test ---

func TestIntegration_CascadeDelete_RemovesSubscriptions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	truncateTables(t)
	createTrackedRepo(t, "golang", "go")

	subRepo := repository.NewSubscriptionRepo(testDB)
	ctx := context.Background()

	sub := &model.Subscription{
		Email: "user@example.com", RepoOwner: "golang", RepoName: "go",
		Token: "tok-cascade", Status: model.StatusPending,
	}
	require.NoError(t, subRepo.Create(ctx, sub))

	// Delete the tracked repo — subscription should cascade delete
	_, err := testDB.ExecContext(ctx, "DELETE FROM tracked_repositories WHERE owner = $1 AND name = $2", "golang", "go")
	require.NoError(t, err)

	_, err = subRepo.GetByToken(ctx, "tok-cascade")
	require.Error(t, err)
	assert.True(t, errors.Is(err, repository.ErrNotFound), "subscription should be cascade-deleted")
}
