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

// --- RepoExists Cache Tests ---

func TestCachedClient_RepoExists_CacheHit(t *testing.T) {
	var apiCalls int32

	client, _ := setupCachedClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&apiCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": 1}`)) //nolint:errcheck // test: write ignored
	}))

	ctx := context.Background()

	// First call — cache miss, hits API
	exists, err := client.RepoExists(ctx, "golang", "go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected repo to exist")
	}

	// Second call — cache hit, should NOT hit API
	exists, err = client.RepoExists(ctx, "golang", "go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected repo to exist from cache")
	}

	if got := atomic.LoadInt32(&apiCalls); got != 1 {
		t.Errorf("API calls = %d, want 1 (second call should be cached)", got)
	}
}

// --- GetLatestRelease Cache Tests ---

func TestCachedClient_GetLatestRelease_CacheHit(t *testing.T) {
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

	// First call — cache miss
	release, err := client.GetLatestRelease(ctx, "test", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release.TagName != "v1.0.0" {
		t.Errorf("tag = %q, want %q", release.TagName, "v1.0.0")
	}

	// Second call — cache hit
	release2, err := client.GetLatestRelease(ctx, "test", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release2.TagName != "v1.0.0" {
		t.Errorf("cached tag = %q, want %q", release2.TagName, "v1.0.0")
	}

	if got := atomic.LoadInt32(&apiCalls); got != 1 {
		t.Errorf("API calls = %d, want 1 (second call should be cached)", got)
	}
}

func TestCachedClient_GetLatestRelease_NoRelease_NotCached(t *testing.T) {
	var apiCalls int32

	client, _ := setupCachedClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&apiCalls, 1)
		w.WriteHeader(http.StatusNotFound)
	}))

	ctx := context.Background()

	release, err := client.GetLatestRelease(ctx, "test", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release != nil {
		t.Errorf("expected nil release, got %+v", release)
	}

	// Second call should still hit API (nil releases are not cached)
	_, _ = client.GetLatestRelease(ctx, "test", "repo") //nolint:errcheck // test: result not needed
	if got := atomic.LoadInt32(&apiCalls); got != 2 {
		t.Errorf("API calls = %d, want 2 (nil releases should not be cached)", got)
	}
}

// --- Graceful Degradation Tests ---

func TestCachedClient_RepoExists_RedisDown_FallsBackToAPI(t *testing.T) {
	var apiCalls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&apiCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": 1}`)) //nolint:errcheck // test: write ignored
	}))
	defer srv.Close()

	base := NewClient("")
	base.baseURL = srv.URL

	// Connect to a non-existent Redis — simulates Redis being down
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:0", MaxRetries: 0, PoolSize: 1, MinIdleConns: 0, DialTimeout: time.Millisecond,
	})
	defer rdb.Close() //nolint:errcheck // close error safe to ignore in test

	cached := NewCachedClient(base, rdb, 5*time.Minute)

	ctx := context.Background()

	// Should succeed despite Redis being down
	exists, err := cached.RepoExists(ctx, "golang", "go")
	if err != nil {
		t.Fatalf("expected graceful degradation, got error: %v", err)
	}
	if !exists {
		t.Error("expected repo to exist")
	}

	// Both calls hit API since cache is unavailable
	exists, err = cached.RepoExists(ctx, "golang", "go")
	if err != nil {
		t.Fatalf("expected graceful degradation on second call: %v", err)
	}
	if !exists {
		t.Error("expected repo to exist")
	}

	if got := atomic.LoadInt32(&apiCalls); got != 2 {
		t.Errorf("API calls = %d, want 2 (both should hit API with Redis down)", got)
	}
}

func TestCachedClient_GetLatestRelease_RedisDown_FallsBackToAPI(t *testing.T) {
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

	release, err := cached.GetLatestRelease(context.Background(), "test", "repo")
	if err != nil {
		t.Fatalf("expected graceful degradation, got error: %v", err)
	}
	if release == nil || release.TagName != "v2.0.0" {
		t.Errorf("expected v2.0.0, got %+v", release)
	}
}

// --- TTL Expiry Test ---

func TestCachedClient_RepoExists_TTLExpiry(t *testing.T) {
	var apiCalls int32

	client, mr := setupCachedClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&apiCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": 1}`)) //nolint:errcheck // test: write ignored
	}))

	ctx := context.Background()

	// First call — populates cache
	_, _ = client.RepoExists(ctx, "golang", "go") //nolint:errcheck // TTL test: call count matters

	// Fast-forward past TTL
	mr.FastForward(6 * time.Minute)

	// Should hit API again after TTL expiry
	_, _ = client.RepoExists(ctx, "golang", "go") //nolint:errcheck // TTL test: call count matters

	if got := atomic.LoadInt32(&apiCalls); got != 2 {
		t.Errorf("API calls = %d, want 2 (cache should expire after TTL)", got)
	}
}
