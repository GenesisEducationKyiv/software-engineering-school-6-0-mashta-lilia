// Package testapp wires the full API surface (HTTP server backed by a
// chi router, real postgres + mailpit containers, in-process fake GitHub
// client) for integration tests, plus the smaller building blocks
// (NewPostgres, SeedSubscription, …) needed by repository-only suites.
package testapp

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"time"

	"github-release-notifier/internal/api/rest"
	"github-release-notifier/internal/api/rest/middleware"
	subhandler "github-release-notifier/internal/api/rest/subscription"
	"github-release-notifier/internal/client/mailer"
	"github-release-notifier/internal/platform/health"
	"github-release-notifier/internal/platform/token"
	"github-release-notifier/internal/repository"
	"github-release-notifier/internal/subscription"
	"github-release-notifier/tests/pkg/testmailpit"
)

// APIKey is the canonical API key seeded into the test router; tests
// authenticate against /api/subscriptions with this value.
const APIKey = "test-api-key-12345"

// App bundles every collaborator an api-level integration test touches —
// the HTTP server, the underlying DB, the captured-mail backend, and the
// programmable GitHub fake.
type App struct {
	Server      *httptest.Server
	DB          *sql.DB
	Mailpit     *testmailpit.Container
	Github      *FakeGithub
	RateLimiter *middleware.RateLimiter
	APIKey      string
}

// New brings up the full API surface and returns a cleanup func that tears
// every container/handle down in reverse order. Designed to be called once
// per suite from TestMain; per-test isolation goes through ResetDB /
// Github.Reset / Mailpit.Reset, not New.
func New(ctx context.Context) (*App, func(), error) {
	var cleanups []func()
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	db, dbCleanup, err := NewPostgres(ctx)
	if err != nil {
		return nil, cleanup, err
	}
	cleanups = append(cleanups, dbCleanup)

	mp, mpCleanup, err := testmailpit.New(ctx)
	if err != nil {
		return nil, cleanup, fmt.Errorf("mailpit: %w", err)
	}
	cleanups = append(cleanups, mpCleanup)

	gh := NewFakeGithub()
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
	mail, err := mailer.NewSMTPMailer(mp.Host, mp.SMTPPort, "", "", "noreply@test.local", templates)
	if err != nil {
		return nil, cleanup, fmt.Errorf("mailer: %w", err)
	}

	svc := subscription.NewService(subRepo, repoStore, gh, mail, token.New())
	handler := subhandler.NewHandler(svc)
	hc := health.NewDBChecker(db)
	router := rest.NewRouter(handler, hc, APIKey, rl, "")
	srv := httptest.NewServer(router)
	cleanups = append(cleanups, srv.Close)

	return &App{
		Server:      srv,
		DB:          db,
		Mailpit:     mp,
		Github:      gh,
		RateLimiter: rl,
		APIKey:      APIKey,
	}, cleanup, nil
}
