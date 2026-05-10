package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github-release-notifier/internal/api/middleware"
	"github-release-notifier/internal/api/rest"
	"github-release-notifier/internal/client/github"
	"github-release-notifier/internal/client/mailer"
	"github-release-notifier/internal/config"
	"github-release-notifier/internal/repository"
	"github-release-notifier/internal/service"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	rateLimitRequests = 10
	httpReadTimeout   = 10 * time.Second
	httpWriteTimeout  = 10 * time.Second
	httpIdleTimeout   = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// dependencies holds the wired application graph that run() needs to start
// the HTTP server and the background scanner. Bundling them lets each
// helper return a single value and keeps run() free of a dozen named locals.
type dependencies struct {
	cfg              *config.Config
	router           http.Handler
	scanner          *service.Scanner
	subscribeLimiter *middleware.RateLimiter
}

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

// run is the application bootstrap. Each phase (DB, migrations, wiring,
// HTTP) is delegated to a focused helper so this function reads as the
// outline of the startup sequence — not the implementation of every step.
func run() error {
	cfg := config.Load()

	db, err := openAndMigrateDB(cfg)
	if err != nil {
		return err
	}
	defer closeQuietly("database", db.Close)

	rdb := newRedisClient(cfg)
	defer closeQuietly("redis", rdb.Close)

	deps := buildDependencies(cfg, db, rdb)

	return runHTTPServer(cfg, deps)
}

// openAndMigrateDB opens the Postgres connection and applies any pending
// schema migrations. It logs the migration outcome.
func openAndMigrateDB(cfg *config.Config) (*sql.DB, error) {
	db, err := repository.NewPostgresDB(cfg.DatabaseURL())
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	migResult, err := repository.RunMigrations(db, "file://migrations")
	if err != nil {
		closeQuietly("database", db.Close)
		return nil, err
	}
	if migResult.Applied {
		slog.Info("migrations applied successfully")
	} else {
		slog.Info("migrations: no changes to apply")
	}
	return db, nil
}

// newRedisClient builds a Redis client from config. It does NOT verify the
// connection — that is the responsibility of buildGitHubClient, which
// decides whether to enable caching based on a Ping result.
func newRedisClient(cfg *config.Config) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
}

// buildGitHubClient returns a service.GitHubClient. If Redis is reachable,
// the returned client is wrapped in a cache-aside decorator; otherwise the
// raw API client is returned (graceful degradation).
//
//nolint:ireturn // composition root: returns the abstraction by design.
func buildGitHubClient(cfg *config.Config, rdb *redis.Client) service.GitHubClient {
	base := github.NewClient(cfg.GitHubToken)
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		slog.Warn("redis unavailable, caching disabled", "error", err)
		return base
	}
	slog.Info("redis connected, caching enabled")
	return github.NewCachedClient(base, rdb, cfg.RedisCacheTTL)
}

// buildDependencies wires every concrete component into the app graph.
// Adding a new dependency means editing one place, not the run() function.
func buildDependencies(cfg *config.Config, db *sql.DB, rdb *redis.Client) *dependencies {
	subRepo := repository.NewSubscriptionRepo(db)
	repoStore := repository.NewTrackedRepoStore(db)
	ghClient := buildGitHubClient(cfg, rdb)

	mailTemplates := mailer.NewTemplateBuilder(cfg.BaseURL)
	mail := mailer.NewSMTPMailer(
		cfg.SMTPHost, cfg.SMTPPort,
		cfg.SMTPUser, cfg.SMTPPassword,
		cfg.SMTPFrom, mailTemplates,
	)

	tokenGen := service.CryptoTokenGenerator{}
	subService := service.NewSubscriptionService(subRepo, repoStore, ghClient, mail, tokenGen)
	scanner, err := service.NewScanner(repoStore, subRepo, ghClient, mail, cfg.ScanInterval)
	if err != nil {
		// scanner only fails on invalid interval — that's a programmer error,
		// not a runtime concern, so panicking here is acceptable.
		panic(fmt.Errorf("creating scanner: %w", err))
	}

	handler := rest.NewHandler(subService)
	healthChecker := service.NewDBHealthChecker(db)
	subscribeLimiter := middleware.NewRateLimiter(rateLimitRequests, time.Minute, cfg.TrustedProxy)
	router := rest.NewRouter(handler, healthChecker, cfg.APIKey, subscribeLimiter, "swagger.yaml")

	return &dependencies{
		cfg:              cfg,
		router:           router,
		scanner:          scanner,
		subscribeLimiter: subscribeLimiter,
	}
}

// runHTTPServer starts the scanner and HTTP server, and blocks until the
// process receives SIGINT/SIGTERM or the server fails. It guarantees an
// orderly shutdown (cancel scanner, stop limiter, drain in-flight requests).
func runHTTPServer(cfg *config.Config, deps *dependencies) error {
	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      deps.router,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: httpWriteTimeout,
		IdleTimeout:  httpIdleTimeout,
	}

	scannerCtx, cancelScanner := context.WithCancel(context.Background())
	defer cancelScanner()
	go deps.scanner.Start(scannerCtx)

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("server starting", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case <-quit:
	}

	slog.Info("shutting down...")
	cancelScanner()
	deps.subscribeLimiter.Stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	slog.Info("server stopped")
	return nil
}

// closeQuietly logs but does not propagate close errors. Used in defer
// statements where the original return error is what matters.
func closeQuietly(name string, closer func() error) {
	if err := closer(); err != nil {
		slog.Error("failed to close "+name, "error", err)
	}
}
