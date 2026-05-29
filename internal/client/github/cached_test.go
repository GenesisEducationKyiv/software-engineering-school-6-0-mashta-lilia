//nolint:testpackage // white-box tests that share mocks within the package
package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github-release-notifier/internal/release"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupCachedClient(t *testing.T, handler http.Handler) (*CachedClient, *miniredis.Miniredis) {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	mr := miniredis.RunT(t)

	base := NewClient("")
	base.baseURL = srv.URL

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() }) //nolint:errcheck,gosec // close error safe to ignore in test cleanup

	cached := NewCachedClient(base, rdb, 5*time.Minute)
	return cached, mr
}

func TestCachedClient_RepoExists_CacheHit(t *testing.T) {
	t.Parallel()
	var apiCalls int32

	client, _ := setupCachedClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&apiCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": 1}`)) //nolint:errcheck // test: write ignored
	}))

	ctx := context.Background()

	exists, err := client.RepoExists(ctx, "golang", "go")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = client.RepoExists(ctx, "golang", "go")
	require.NoError(t, err)
	assert.True(t, exists)

	assert.Equal(t, int32(1), atomic.LoadInt32(&apiCalls),
		"second call must be served from cache")
}

func TestCachedClient_GetLatestRelease_CacheHit(t *testing.T) {
	t.Parallel()
	var apiCalls int32

	client, _ := setupCachedClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&apiCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(release.Release{ //nolint:errcheck // test: encode ignored
			TagName: "v1.0.0",
			Name:    "Release 1.0",
			HTMLURL: "https://github.com/test/repo/releases/tag/v1.0.0",
		})
	}))

	ctx := context.Background()

	rel, err := client.GetLatestRelease(ctx, "test", "repo")
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", rel.TagName)

	rel2, err := client.GetLatestRelease(ctx, "test", "repo")
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", rel2.TagName)

	assert.Equal(t, int32(1), atomic.LoadInt32(&apiCalls),
		"second call must be served from cache")
}

func TestCachedClient_GetLatestRelease_NoRelease_NotCached(t *testing.T) {
	t.Parallel()
	var apiCalls int32

	client, _ := setupCachedClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&apiCalls, 1)
		w.WriteHeader(http.StatusNotFound)
	}))

	ctx := context.Background()

	rel, err := client.GetLatestRelease(ctx, "test", "repo")
	require.NoError(t, err)
	assert.Nil(t, rel)

	_, _ = client.GetLatestRelease(ctx, "test", "repo") //nolint:errcheck // test: result not needed
	assert.Equal(t, int32(2), atomic.LoadInt32(&apiCalls),
		"nil releases must NOT be cached — second call must hit API")
}

func TestCachedClient_RepoExists_RedisDown_FallsBackToAPI(t *testing.T) {
	t.Parallel()
	var apiCalls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&apiCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": 1}`)) //nolint:errcheck // test: write ignored
	}))
	defer srv.Close()

	base := NewClient("")
	base.baseURL = srv.URL

	// localhost:0 simulates Redis down.
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:0", MaxRetries: 0, PoolSize: 1, MinIdleConns: 0, DialTimeout: time.Millisecond,
	})
	defer rdb.Close() //nolint:errcheck // close error safe to ignore in test

	cached := NewCachedClient(base, rdb, 5*time.Minute)

	ctx := context.Background()

	exists, err := cached.RepoExists(ctx, "golang", "go")
	require.NoError(t, err, "expected graceful degradation when Redis is down")
	assert.True(t, exists)

	exists, err = cached.RepoExists(ctx, "golang", "go")
	require.NoError(t, err, "expected graceful degradation on second call")
	assert.True(t, exists)

	assert.Equal(t, int32(2), atomic.LoadInt32(&apiCalls),
		"both calls must hit API when Redis is down")
}

func TestCachedClient_GetLatestRelease_RedisDown_FallsBackToAPI(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		r := release.Release{TagName: "v2.0.0"}
		_ = json.NewEncoder(w).Encode(r) //nolint:errcheck // test: encode ignored
	}))
	defer srv.Close()

	base := NewClient("")
	base.baseURL = srv.URL

	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:0", MaxRetries: 0, PoolSize: 1, MinIdleConns: 0, DialTimeout: time.Millisecond,
	})
	defer rdb.Close() //nolint:errcheck // close error safe to ignore in test

	cached := NewCachedClient(base, rdb, 5*time.Minute)

	rel, err := cached.GetLatestRelease(context.Background(), "test", "repo")
	require.NoError(t, err, "expected graceful degradation when Redis is down")
	require.NotNil(t, rel)
	assert.Equal(t, "v2.0.0", rel.TagName)
}

func TestCachedClient_RepoExists_TTLExpiry(t *testing.T) {
	t.Parallel()
	var apiCalls int32

	client, mr := setupCachedClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&apiCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": 1}`)) //nolint:errcheck // test: write ignored
	}))

	ctx := context.Background()

	_, _ = client.RepoExists(ctx, "golang", "go") //nolint:errcheck // TTL test: call count matters
	mr.FastForward(6 * time.Minute)
	_, _ = client.RepoExists(ctx, "golang", "go") //nolint:errcheck // TTL test: call count matters

	assert.Equal(t, int32(2), atomic.LoadInt32(&apiCalls),
		"cache must expire after TTL — second call should hit API")
}
