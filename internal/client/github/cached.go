package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/release"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"
)

// Per-failure logs are Debug to avoid flap-noise; alert on this counter instead.
var redisCacheErrors = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "github_cache_redis_errors_total",
		Help: "Redis-side errors observed by the GitHub cache decorator, by operation.",
	},
	[]string{"op"},
)

type baseClient interface {
	RepoExists(ctx context.Context, owner, name string) (bool, error)
	GetLatestRelease(ctx context.Context, owner, name string) (*release.Release, error)
}

type CachedClient struct {
	base  baseClient
	redis *redis.Client
	ttl   time.Duration
	log   logger.Logger
}

func NewCachedClient(
	base baseClient, rdb *redis.Client, ttl time.Duration, logs ...logger.Logger,
) *CachedClient {
	return &CachedClient{base: base, redis: rdb, ttl: ttl, log: logger.Or(logs...)}
}

func (c *CachedClient) RepoExists(ctx context.Context, owner, name string) (bool, error) {
	key := fmt.Sprintf("github:repo_exists:%s/%s", owner, name)

	val, err := c.redis.Get(ctx, key).Result()
	if err == nil {
		return val == "1", nil
	}
	if !errors.Is(err, redis.Nil) {
		redisCacheErrors.WithLabelValues("get_repo").Inc()
		c.log.Debug(ctx, "redis_get_failed", "op", "repo_exists", "err", err)
	}

	exists, err := c.base.RepoExists(ctx, owner, name)
	if err != nil {
		return false, err
	}

	// Negative results not cached — would mask a freshly-created repo.
	if exists {
		if setErr := c.redis.Set(ctx, key, "1", c.ttl).Err(); setErr != nil {
			redisCacheErrors.WithLabelValues("set_repo").Inc()
			c.log.Debug(ctx, "redis_set_failed", "op", "repo_exists", "err", setErr)
		}
	}

	return exists, nil
}

func (c *CachedClient) GetLatestRelease(ctx context.Context, owner, name string) (*release.Release, error) {
	key := fmt.Sprintf("github:release:%s/%s", owner, name)

	val, err := c.redis.Get(ctx, key).Result()
	if err == nil {
		var rel release.Release
		unmarshalErr := json.Unmarshal([]byte(val), &rel)
		if unmarshalErr == nil {
			return &rel, nil
		}
		redisCacheErrors.WithLabelValues("unmarshal_release").Inc()
		c.log.Debug(ctx, "redis_unmarshal_failed", "op", "release", "err", unmarshalErr)
	} else if !errors.Is(err, redis.Nil) {
		redisCacheErrors.WithLabelValues("get_release").Inc()
		c.log.Debug(ctx, "redis_get_failed", "op", "release", "err", err)
	}

	rel, err := c.base.GetLatestRelease(ctx, owner, name)
	if err != nil {
		return nil, err
	}
	if rel == nil {
		return nil, nil
	}

	if data, marshalErr := json.Marshal(rel); marshalErr == nil {
		if setErr := c.redis.Set(ctx, key, data, c.ttl).Err(); setErr != nil {
			redisCacheErrors.WithLabelValues("set_release").Inc()
			c.log.Debug(ctx, "redis_set_failed", "op", "release", "err", setErr)
		}
	} else {
		redisCacheErrors.WithLabelValues("marshal_release").Inc()
		c.log.Debug(ctx, "redis_marshal_failed", "op", "release", "err", marshalErr)
	}

	return rel, nil
}
