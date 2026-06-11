// Package testapp wires the full API surface for integration tests.
package testapp

import (
	"context"
	"database/sql"
	"fmt"
	"github-release-notifier/internal/api/rest"
	"github-release-notifier/internal/api/rest/middleware"
	notificationv1 "github-release-notifier/internal/gen/notification/v1"
	notificationclient "github-release-notifier/internal/outbound/notification"
	"github-release-notifier/internal/platform/health"
	"github-release-notifier/internal/platform/logger"
	platformpostgres "github-release-notifier/internal/platform/postgres"
	"github-release-notifier/internal/platform/token"
	"github-release-notifier/internal/repository"
	"github-release-notifier/internal/subscription"
	notificationapp "github-release-notifier/services/notification/app"
	"github-release-notifier/services/notification/inbound/grpcserver"
	notificationsmtp "github-release-notifier/services/notification/outbound/smtp"
	notificationstore "github-release-notifier/services/notification/outbound/store"
	"github-release-notifier/tests/pkg/testdb"
	"github-release-notifier/tests/pkg/testgithub"
	"github-release-notifier/tests/pkg/testmailpit"
	"log/slog"
	"net"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"time"

	subhandler "github-release-notifier/internal/api/rest/subscription"

	"github.com/testcontainers/testcontainers-go"
	testpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
)

const (
	// Set well above per-test traffic so the limiter never becomes a confounder.
	rateLimitRequests       = 100
	rateLimitWindow         = time.Minute
	postgresReadyOccurrence = 2
	postgresStartupTimeout  = 60 * time.Second
)

const APIKey = "test-api-key-12345"

type App struct {
	Server      *httptest.Server
	DB          *sql.DB
	Mailpit     *testmailpit.Container
	Github      *testgithub.Fake
	RateLimiter *middleware.RateLimiter
	APIKey      string
}

// Per-test isolation goes through testdb.TruncateAll / Github.Reset / Mailpit.Reset, not New.
func New(ctx context.Context) (*App, func(), error) {
	var cleanups []func()
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	db, dbCleanup, err := testdb.NewPostgres(ctx)
	if err != nil {
		return nil, cleanup, err
	}
	cleanups = append(cleanups, dbCleanup)

	mp, mpCleanup, err := testmailpit.New(ctx)
	if err != nil {
		return nil, cleanup, fmt.Errorf("mailpit: %w", err)
	}
	cleanups = append(cleanups, mpCleanup)

	gh := testgithub.New()
	log := logger.New(logger.Config{Level: "warn", ServiceName: "test"})
	rl := middleware.NewRateLimiter(rateLimitRequests, rateLimitWindow, false, log)
	cleanups = append(cleanups, rl.Stop)

	subRepo, err := subscription.NewRepoWithContext(ctx, db, log)
	if err != nil {
		return nil, cleanup, fmt.Errorf("subscription repo: %w", err)
	}
	cleanups = append(cleanups, func() {
		if err := subRepo.Close(); err != nil {
			slog.Warn("close subscription repo", "err", err)
		}
	})

	repoStore, err := repository.NewStoreWithContext(ctx, db, log)
	if err != nil {
		return nil, cleanup, fmt.Errorf("tracked repo store: %w", err)
	}
	cleanups = append(cleanups, func() {
		if err := repoStore.Close(); err != nil {
			slog.Warn("close repo store", "err", err)
		}
	})

	notifier, notifierCleanup, err := newNotificationClient(ctx, mp, log)
	// Registered before the error check so a partial failure still tears down what started.
	cleanups = append(cleanups, notifierCleanup)
	if err != nil {
		return nil, cleanup, fmt.Errorf("notification client: %w", err)
	}

	svc := subscription.NewService(subRepo, repoStore, gh, notifier, token.New())
	handler := subhandler.NewHandler(svc, log)
	hc := health.NewDBChecker(db)
	router := rest.NewRouter(handler, hc, APIKey, rl, "", log)
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

func newNotificationClient(
	ctx context.Context, mp *testmailpit.Container, log *logger.Logger,
) (*notificationclient.Client, func(), error) {
	var cleanups []func()
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	notificationDB, notificationDBCleanup, err := newNotificationDB(ctx)
	// Registered before the error check so a started container is still terminated on later failure.
	cleanups = append(cleanups, notificationDBCleanup)
	if err != nil {
		return nil, cleanup, fmt.Errorf("notification db: %w", err)
	}

	ledger, err := notificationstore.NewWithContext(ctx, notificationDB, log)
	if err != nil {
		return nil, cleanup, fmt.Errorf("notification store: %w", err)
	}
	cleanups = append(cleanups, func() {
		if err := ledger.Close(); err != nil {
			slog.Warn("close notification store", "err", err)
		}
	})

	templates := notificationsmtp.NewTemplateBuilder("http://test.local")
	mail, err := notificationsmtp.NewSMTPMailer(
		mp.Host, mp.SMTPPort, "", "", "noreply@test.local", templates,
	)
	if err != nil {
		return nil, cleanup, fmt.Errorf("notification smtp: %w", err)
	}
	notificationService, err := notificationapp.NewService(mail, ledger, log)
	if err != nil {
		return nil, cleanup, fmt.Errorf("notification service: %w", err)
	}

	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, cleanup, fmt.Errorf("notification listener: %w", err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(grpcserver.TraceUnaryServerInterceptor()))
	notificationv1.RegisterNotificationServiceServer(server, grpcserver.New(notificationService))
	go func() {
		if err := server.Serve(listener); err != nil {
			slog.Warn("notification test server stopped", "err", err)
		}
	}()
	cleanups = append(cleanups, server.Stop)

	conn, client, err := notificationclient.Dial(listener.Addr().String(), log)
	if err != nil {
		return nil, cleanup, fmt.Errorf("notification dial: %w", err)
	}
	cleanups = append(cleanups, func() {
		if err := conn.Close(); err != nil {
			slog.Warn("close notification grpc conn", "err", err)
		}
	})

	return client, cleanup, nil
}

func newNotificationDB(ctx context.Context) (*sql.DB, func(), error) {
	container, err := testpostgres.Run(
		ctx,
		"postgres:16-alpine",
		testpostgres.WithDatabase("notification"),
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
			slog.Warn("terminate notification postgres", "err", err)
		}
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, terminate, err
	}
	migrationsURL := notificationMigrationsURL()
	if _, err := platformpostgres.RunMigrationsWithContext(ctx, connStr, migrationsURL); err != nil {
		return nil, terminate, err
	}
	db, err := platformpostgres.NewWithContext(ctx, connStr)
	if err != nil {
		return nil, terminate, err
	}

	cleanup := func() {
		if err := db.Close(); err != nil {
			slog.Warn("close notification test db", "err", err)
		}
		terminate()
	}
	return db, cleanup, nil
}

func notificationMigrationsURL() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot resolve testapp package directory")
	}
	path, err := filepath.Abs(
		filepath.Join(filepath.Dir(file), "..", "..", "..", "services", "notification", "migrations"),
	)
	if err != nil {
		panic(err)
	}
	return "file://" + filepath.ToSlash(path)
}
