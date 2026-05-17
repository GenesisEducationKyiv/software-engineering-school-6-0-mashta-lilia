package app

import (
	"context"
	"database/sql"
	"fmt"
	"github-release-notifier/internal/api/rest"
	"github-release-notifier/internal/api/rest/middleware"
	"github-release-notifier/internal/client/github"
	"github-release-notifier/internal/client/mailer"
	"github-release-notifier/internal/config"
	"github-release-notifier/internal/platform/health"
	"github-release-notifier/internal/platform/token"
	"github-release-notifier/internal/release"
	"github-release-notifier/internal/storage"
	"github-release-notifier/internal/subscription"
	"log/slog"
	"net/http"
	"time"

	subhandler "github-release-notifier/internal/api/rest/subscription"

	"github.com/redis/go-redis/v9"
)

const (
	rateLimitRequests = 10
	redisPingTimeout  = 2 * time.Second
)

type dependencies struct {
	router           http.Handler
	poller           *release.Poller
	subscribeLimiter *middleware.RateLimiter
}

func newRedisClient(cfg *config.Config) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
}

func buildDependencies(
	ctx context.Context, cfg *config.Config, db *sql.DB, rdb *redis.Client,
) (*dependencies, error) {
	subRepo := storage.NewSubscriptionRepo(db)
	repoStore := storage.NewTrackedRepoStore(db)

	base := github.NewClient(cfg.GitHubToken)
	ghClient := selectGitHubClient(ctx, base, rdb, cfg.RedisCacheTTL)

	mailTemplates := mailer.NewTemplateBuilder(cfg.BaseURL)
	mail := mailer.NewSMTPMailer(
		cfg.SMTPHost, cfg.SMTPPort,
		cfg.SMTPUser, cfg.SMTPPassword,
		cfg.SMTPFrom, mailTemplates,
	)

	tokenGen := token.New()
	subService := subscription.NewService(subRepo, repoStore, ghClient, mail, tokenGen)

	poller, err := release.NewPoller(repoStore, subRepo, ghClient, mail, cfg.ScanInterval)
	if err != nil {
		return nil, fmt.Errorf("creating poller: %w", err)
	}

	handler := subhandler.NewHandler(subService)
	healthChecker := health.NewDBChecker(db)
	subscribeLimiter := middleware.NewRateLimiter(rateLimitRequests, time.Minute, cfg.TrustedProxy)

	if cfg.APIKey == "" {
		// APIKey middleware bypasses auth on empty key — /api/subscriptions
		// becomes a public PII endpoint. Surface loudly so a misconfigured
		// prod deploy is impossible to miss.
		slog.Warn("API_KEY is not set — /api/subscriptions is unauthenticated")
	}
	router := rest.NewRouter(handler, healthChecker, cfg.APIKey, subscribeLimiter, "swagger.yaml")

	return &dependencies{
		router:           router,
		poller:           poller,
		subscribeLimiter: subscribeLimiter,
	}, nil
}

type githubClient interface {
	RepoExists(ctx context.Context, owner, name string) (bool, error)
	GetLatestRelease(ctx context.Context, owner, name string) (*release.Release, error)
}

func selectGitHubClient( //nolint:ireturn // composition root chooses between concrete impls
	ctx context.Context, base *github.Client, rdb *redis.Client, ttl time.Duration,
) githubClient {
	pingCtx, cancel := context.WithTimeout(ctx, redisPingTimeout)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		slog.Warn("Redis unavailable, caching disabled", "err", err)
		return base
	}
	slog.Info("Redis connected, caching enabled")
	return github.NewCachedClient(base, rdb, ttl)
}
