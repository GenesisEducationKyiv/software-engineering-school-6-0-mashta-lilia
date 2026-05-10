package main

import (
	"context"
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

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()

	// Database
	db, err := repository.NewPostgresDB(cfg.DatabaseURL())
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("failed to close database", "error", err)
		}
	}()

	// Migrations
	migResult, err := repository.RunMigrations(db, "file://migrations")
	if err != nil {
		return err
	}
	if migResult.Applied {
		slog.Info("migrations applied successfully")
	} else {
		slog.Info("migrations: no changes to apply")
	}

	// Dependencies
	subRepo := repository.NewSubscriptionRepo(db)
	repoStore := repository.NewTrackedRepoStore(db)
	baseGHClient := github.NewClient(cfg.GitHubToken)
	mail := mailer.NewSMTPMailer(
		cfg.SMTPHost, cfg.SMTPPort,
		cfg.SMTPUser, cfg.SMTPPassword,
		cfg.SMTPFrom, cfg.BaseURL,
	)

	// Redis cache layer (graceful degradation if unavailable)
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer func() {
		if err := rdb.Close(); err != nil {
			slog.Error("failed to close redis", "error", err)
		}
	}()

	var ghClient service.GitHubClient
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		slog.Warn("redis unavailable, caching disabled", "error", err)
		ghClient = baseGHClient
	} else {
		slog.Info("redis connected, caching enabled")
		ghClient = github.NewCachedClient(baseGHClient, rdb, cfg.RedisCacheTTL)
	}

	// Services
	subService := service.NewSubscriptionService(subRepo, repoStore, ghClient, mail)
	scanner, err := service.NewScanner(repoStore, subRepo, ghClient, mail, cfg.ScanInterval)
	if err != nil {
		return fmt.Errorf("creating scanner: %w", err)
	}

	// HTTP
	handler := rest.NewHandler(subService)
	healthChecker := service.NewDBHealthChecker(db)
	subscribeLimiter := middleware.NewRateLimiter(rateLimitRequests, time.Minute, cfg.TrustedProxy)
	router := rest.NewRouter(handler, healthChecker, cfg.APIKey, subscribeLimiter, "swagger.yaml")

	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      router,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: httpWriteTimeout,
		IdleTimeout:  httpIdleTimeout,
	}

	// Start scanner in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go scanner.Start(ctx)

	// Start server
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("server starting", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case <-quit:
	}

	slog.Info("shutting down...")
	cancel()
	subscribeLimiter.Stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	slog.Info("server stopped")

	return nil
}
