package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github-release-notifier/internal/api/middleware"
	"github-release-notifier/internal/api/rest"
	"github-release-notifier/internal/client/github"
	"github-release-notifier/internal/client/mailer"
	"github-release-notifier/internal/config"
	"github-release-notifier/internal/repository"
	"github-release-notifier/internal/service"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()

	// Database
	db, err := repository.NewPostgresDB(cfg.DatabaseURL())
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	// Migrations
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		log.Fatalf("failed to create migration driver: %v", err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://migrations", "postgres", driver)
	if err != nil {
		log.Fatalf("failed to create migrator: %v", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatalf("failed to run migrations: %v", err)
	}
	log.Println("migrations applied successfully")

	// Dependencies
	subRepo := repository.NewSubscriptionRepo(db)
	repoStore := repository.NewTrackedRepoStore(db)
	baseGHClient := github.NewClient(cfg.GitHubToken)
	mail := mailer.NewSMTPMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPassword, cfg.SMTPFrom, cfg.BaseURL)

	// Redis cache layer (graceful degradation if unavailable)
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Printf("redis unavailable, caching disabled: %v", err)
	} else {
		log.Println("redis connected, caching enabled")
	}
	ghClient := github.NewCachedClient(baseGHClient, rdb, cfg.RedisCacheTTL)

	// Services
	subService := service.NewSubscriptionService(subRepo, repoStore, ghClient, mail)
	scanner := service.NewScanner(repoStore, subRepo, ghClient, mail, cfg.ScanInterval)

	// HTTP
	handler := rest.NewHandler(subService)
	subscribeLimiter := middleware.NewRateLimiter(10, time.Minute, cfg.TrustedProxy)
	router := rest.NewRouter(handler, db, cfg.APIKey, subscribeLimiter, "swagger.yaml")

	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start scanner in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go scanner.Start(ctx)

	// Start server
	go func() {
		log.Printf("server starting on :%s", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")
	cancel()
	subscribeLimiter.Stop()
	rdb.Close()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown error: %v", err)
	}
	log.Println("server stopped")
}
