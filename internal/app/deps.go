package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github-release-notifier/internal/api/rest"
	"github-release-notifier/internal/api/rest/middleware"
	"github-release-notifier/internal/client/github"
	"github-release-notifier/internal/client/mailer"
	"github-release-notifier/internal/config"
	"github-release-notifier/internal/platform/health"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/platform/token"
	"github-release-notifier/internal/release"
	"github-release-notifier/internal/repository"
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
	closers          []func() error
}

func newRedisClient(cfg *config.Config) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
}

func buildDependencies(
	ctx context.Context, cfg *config.Config, db *sql.DB, rdb *redis.Client, log *logger.Logger,
) (*dependencies, error) {
	subRepo, err := subscription.NewRepoWithContext(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("creating subscription repo: %w", err)
	}
	repoStore, err := repository.NewStoreWithContext(ctx, db)
	if err != nil {
		closeQuietly("subscription repo", subRepo.Close)
		return nil, fmt.Errorf("creating tracked repo store: %w", err)
	}
	storageReady := false
	defer func() {
		if !storageReady {
			closeQuietly("subscription repo", subRepo.Close)
			closeQuietly("tracked repo store", repoStore.Close)
		}
	}()

	base := github.NewClient(cfg.GitHubToken)
	ghClient := selectGitHubClient(ctx, base, rdb, cfg.RedisCacheTTL)

	mailTemplates := mailer.NewTemplateBuilder(cfg.BaseURL)
	mail, err := mailer.NewSMTPMailer(
		cfg.SMTPHost, cfg.SMTPPort,
		cfg.SMTPUser, cfg.SMTPPassword,
		cfg.SMTPFrom, mailTemplates,
	)
	if err != nil {
		return nil, fmt.Errorf("creating mailer: %w", err)
	}

	tokenGen := token.New()
	subService := subscription.NewService(subRepo, repoStore, ghClient, mail, tokenGen)

	poller, err := release.NewPoller(repoStore, subRepo, ghClient, mail, cfg.ScanInterval)
	if err != nil {
		return nil, fmt.Errorf("creating poller: %w", err)
	}

	handler := subhandler.NewHandler(subService)
	healthChecker := health.NewDBChecker(db)
	subscribeLimiter := middleware.NewRateLimiter(rateLimitRequests, time.Minute, cfg.TrustedProxy)

	router := rest.NewRouter(handler, healthChecker, cfg.APIKey, subscribeLimiter, "swagger.yaml", log)

	storageReady = true
	return &dependencies{
		router:           router,
		poller:           poller,
		subscribeLimiter: subscribeLimiter,
		closers:          []func() error{subRepo.Close, repoStore.Close},
	}, nil
}

func (d *dependencies) Close() error {
	if d == nil {
		return nil
	}
	var err error
	for _, closeFn := range d.closers {
		err = errors.Join(err, closeFn())
	}
	return err
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
