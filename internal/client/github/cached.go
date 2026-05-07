package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github-release-notifier/internal/model"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// gitHubClient is the subset of functionality that CachedClient delegates to.
// Defined locally to avoid coupling to the service-layer interface.
type gitHubClient interface {
	RepoExists(ctx context.Context, owner, name string) (bool, error)
	GetLatestRelease(ctx context.Context, owner, name string) (*model.Release, error)
}

// CachedClient wraps a base GitHubClient with a Redis cache-aside layer.
// If Redis is unavailable, it gracefully degrades to direct API calls.
type CachedClient struct {
	base  gitHubClient
	redis *redis.Client
	ttl   time.Duration
}

func NewCachedClient(base gitHubClient, rdb *redis.Client, ttl time.Duration) *CachedClient {
	return &CachedClient{
		base:  base,
		redis: rdb,
		ttl:   ttl,
	}
}

func (c *CachedClient) RepoExists(ctx context.Context, owner, name string) (bool, error) {
	key := fmt.Sprintf("github:repo_exists:%s/%s", owner, name)

	val, err := c.redis.Get(ctx, key).Result()
	if err == nil {
		return val == "1", nil
	}
	if !errors.Is(err, redis.Nil) {
		slog.Warn("redis get error (repo_exists)", "error", err)
	}

	exists, err := c.base.RepoExists(ctx, owner, name)
	if err != nil {
		return false, err
	}

	// Only cache positive results. Caching "repo not found" would cause
	// a stale 404 if the user creates the repo and retries within the TTL.
	if exists {
		if setErr := c.redis.Set(ctx, key, "1", c.ttl).Err(); setErr != nil {
			slog.Warn("redis set error (repo_exists)", "error", setErr)
		}
	}

	return exists, nil
}

func (c *CachedClient) GetLatestRelease(ctx context.Context, owner, name string) (*model.Release, error) {
	key := fmt.Sprintf("github:release:%s/%s", owner, name)

	val, err := c.redis.Get(ctx, key).Result()
	if err == nil {
		var release model.Release
		unmarshalErr := json.Unmarshal([]byte(val), &release)
		if unmarshalErr == nil {
			return &release, nil
		}
		slog.Warn("redis unmarshal error (release)", "error", unmarshalErr)
	} else if !errors.Is(err, redis.Nil) {
		slog.Warn("redis get error (release)", "error", err)
	}

	release, err := c.base.GetLatestRelease(ctx, owner, name)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, nil
	}

	data, marshalErr := json.Marshal(release)
	if marshalErr == nil {
		if setErr := c.redis.Set(ctx, key, data, c.ttl).Err(); setErr != nil {
			slog.Warn("redis set error (release)", "error", setErr)
		}
	}

	return release, nil
}
