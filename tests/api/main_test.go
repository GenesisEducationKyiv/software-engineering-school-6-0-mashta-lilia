package api_test

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github-release-notifier/internal/api/rest"
	"github-release-notifier/internal/api/rest/middleware"
	subhandler "github-release-notifier/internal/api/rest/subscription"
	"github-release-notifier/internal/client/mailer"
	"github-release-notifier/internal/platform/health"
	"github-release-notifier/internal/platform/token"
	"github-release-notifier/internal/release"
	"github-release-notifier/internal/repository"
	"github-release-notifier/internal/subscription"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testEnv carries everything an api integration test needs: a running HTTP
// server backed by a real Chi router, a real Postgres (testcontainers), a
// real Mailpit SMTP server (testcontainers) we can query for captured mail,
// and an in-process fake GitHub client we can program per-test.
type testEnv struct {
	server      *httptest.Server
	db          *sql.DB
	mailpit     *mailpitContainer
	github      *fakeGithub
	rateLimiter *middleware.RateLimiter

	// APIKey for /api/subscriptions; tests override headers as needed.
	apiKey string
}

var (
	sharedEnv *testEnv
	setupErr  error
)

const testAPIKey = "test-api-key-12345"

func TestMain(m *testing.M) {
	// All container lifecycle is funneled through runTests so deferred
	// cleanup runs even on mid-setup failures; calling os.Exit from
	// TestMain directly would skip those defers and leak containers.
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}

	env, cleanup, err := setupEnv(context.Background())
	defer cleanup()
	if err != nil {
		// Record the cause so envForTest can report it to each skipped/
		// failed test rather than logging it once at startup and losing it.
		setupErr = err
		slog.Error("integration setup failed", "err", err)
		return 1
	}
	sharedEnv = env

	return m.Run()
}

// envForTest returns the shared environment, skipping if -short or setup failed.
func envForTest(t *testing.T) *testEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if sharedEnv == nil {
		t.Fatalf("integration env not initialized; setup error: %v", setupErr)
	}
	return sharedEnv
}

func setupEnv(ctx context.Context) (*testEnv, func(), error) {
	cleanups := []func(){}
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	db, dbCleanup, err := startPostgres(ctx)
	if err != nil {
		return nil, cleanup, fmt.Errorf("postgres: %w", err)
	}
	cleanups = append(cleanups, dbCleanup)

	mp, mpCleanup, err := startMailpit(ctx)
	if err != nil {
		return nil, cleanup, fmt.Errorf("mailpit: %w", err)
	}
	cleanups = append(cleanups, mpCleanup)

	gh := newFakeGithub()
	rl := middleware.NewRateLimiter(100, time.Minute, false)
	cleanups = append(cleanups, rl.Stop)

	subRepo, err := subscription.NewRepoWithContext(ctx, db)
	if err != nil {
		return nil, cleanup, fmt.Errorf("subscription repo: %w", err)
	}
	cleanups = append(cleanups, func() {
		if err := subRepo.Close(); err != nil {
			slog.Warn("close subscription repo", "err", err)
		}
	})

	repoStore, err := repository.NewStoreWithContext(ctx, db)
	if err != nil {
		return nil, cleanup, fmt.Errorf("tracked repo store: %w", err)
	}
	cleanups = append(cleanups, func() {
		if err := repoStore.Close(); err != nil {
			slog.Warn("close repo store", "err", err)
		}
	})

	templates := mailer.NewTemplateBuilder("http://test.local")
	mail, err := mailer.NewSMTPMailer(mp.host, mp.smtpPort, "", "", "noreply@test.local", templates)
	if err != nil {
		return nil, cleanup, fmt.Errorf("mailer: %w", err)
	}

	svc := subscription.NewService(subRepo, repoStore, gh, mail, token.New())
	handler := subhandler.NewHandler(svc)
	hc := health.NewDBChecker(db)
	router := rest.NewRouter(handler, hc, testAPIKey, rl, "")
	srv := httptest.NewServer(router)
	cleanups = append(cleanups, srv.Close)

	return &testEnv{
		server:      srv,
		db:          db,
		mailpit:     mp,
		github:      gh,
		rateLimiter: rl,
		apiKey:      testAPIKey,
	}, cleanup, nil
}

func startPostgres(ctx context.Context) (*sql.DB, func(), error) {
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
		return nil, func() {}, err
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

	return db, func() {
		_ = db.Close()
		terminate()
	}, nil
}

func runMigrations(db *sql.DB) error {
	driver, err := migratepg.WithInstance(db, &migratepg.Config{})
	if err != nil {
		return fmt.Errorf("migrate driver: %w", err)
	}
	// Tests live under tests/api, migrations are at the repo root.
	abs, err := filepath.Abs(filepath.Join("..", "..", "migrations"))
	if err != nil {
		return fmt.Errorf("migrations path: %w", err)
	}
	mig, err := migrate.NewWithDatabaseInstance("file://"+filepath.ToSlash(abs), "postgres", driver)
	if err != nil {
		return fmt.Errorf("migrate new: %w", err)
	}
	defer func() {
		srcErr, dbErr := mig.Close()
		if srcErr != nil {
			slog.Warn("close migration source", "err", srcErr)
		}
		if dbErr != nil {
			slog.Warn("close migration db", "err", dbErr)
		}
	}()
	if err := mig.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// resetDB truncates all rows between tests so each one runs against a clean
// slate. Cascades remove subscription rows when their tracked repo is purged.
func (e *testEnv) resetDB(t *testing.T) {
	t.Helper()
	_, err := e.db.ExecContext(context.Background(),
		"TRUNCATE subscriptions, tracked_repositories CASCADE")
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// seedSubscription inserts a tracked repo + subscription row directly via
// raw SQL, bypassing the service layer. Used by Confirm/Unsubscribe/List
// tests that need a known token without going through Subscribe.
//
// Raw SQL (not the repo abstraction) so the row lands in its final state
// in a single write — the equivalent helper in e2e/fixtures/seed.ts does
// the same, and we avoid two-step Create+UpdateStatus side-effects (extra
// trigger fires, updated_at advanced past created_at).
func (e *testEnv) seedSubscription(
	t *testing.T, email, owner, name, token string, status subscription.Status,
) int64 {
	t.Helper()
	ctx := context.Background()

	if _, err := e.db.ExecContext(
		ctx,
		`INSERT INTO tracked_repositories (owner, name)
		 VALUES ($1, $2)
		 ON CONFLICT (owner, name) DO NOTHING`,
		owner, name,
	); err != nil {
		t.Fatalf("upsert tracked repo: %v", err)
	}

	var id int64
	err := e.db.QueryRowContext(
		ctx,
		`INSERT INTO subscriptions (email, repo_owner, repo_name, token, status)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		email, owner, name, token, status,
	).Scan(&id)
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	return id
}

// statusOf reads the current status of a subscription by token directly
// from the DB (bypasses the service layer for assertion-side verification).
func (e *testEnv) statusOf(t *testing.T, tok string) subscription.Status {
	t.Helper()
	var s string
	err := e.db.QueryRowContext(context.Background(),
		"SELECT status FROM subscriptions WHERE token = $1", tok).Scan(&s)
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	return subscription.Status(s)
}

// --- Fake GitHub client ---

// fakeGithub satisfies the subscription package's `githubChecker` interface
// purely in-process. Programmable per-test via SetRepo / SetError so we
// don't hit the real GitHub API in integration tests (that's the GitHub
// client's own unit-test surface — see internal/client/github/*_test.go).
type fakeGithub struct {
	mu     sync.Mutex
	exists map[string]bool
	err    error
}

func newFakeGithub() *fakeGithub {
	return &fakeGithub{exists: map[string]bool{}}
}

func (f *fakeGithub) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exists = map[string]bool{}
	f.err = nil
}

func (f *fakeGithub) SetRepoExists(owner, name string, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exists[owner+"/"+name] = ok
}

func (f *fakeGithub) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeGithub) RepoExists(_ context.Context, owner, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	return f.exists[owner+"/"+name], nil
}

// GetLatestRelease is not exercised through HTTP endpoints (the release
// poller uses it), but the interface mirrors the real client so we satisfy
// both contracts and stay forward-compatible.
func (f *fakeGithub) GetLatestRelease(_ context.Context, _, _ string) (*release.Release, error) {
	return nil, nil
}
