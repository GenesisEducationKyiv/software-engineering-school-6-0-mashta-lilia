//nolint:testpackage // white-box tests that share mocks within the package
package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRepoExists_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/golang/go" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/vnd.github.v3+json" {
			t.Error("missing Accept header")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": 1}`)) //nolint:errcheck // test: write ignored
	}))
	defer srv.Close()

	c := NewClient("")
	c.baseURL = srv.URL

	exists, err := c.RepoExists(context.Background(), "golang", "go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected repo to exist")
	}
}

func TestRepoExists_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient("")
	c.baseURL = srv.URL

	exists, err := c.RepoExists(context.Background(), "nonexistent", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected repo to not exist")
	}
}

func TestRepoExists_WithToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient("ghp_testtoken123")
	c.baseURL = srv.URL

	_, err := c.RepoExists(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer ghp_testtoken123" {
		t.Errorf("auth header = %q, want %q", gotAuth, "Bearer ghp_testtoken123")
	}
}

func TestGetLatestRelease_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/golang/go/releases/latest" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tag_name":"v1.22.0","name":"Go 1.22","html_url":"https://github.com/golang/go/releases/tag/v1.22.0"}`)) //nolint:errcheck,revive // test: write ignored, URL exceeds limit
	}))
	defer srv.Close()

	c := NewClient("")
	c.baseURL = srv.URL

	release, err := c.GetLatestRelease(context.Background(), "golang", "go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release == nil {
		t.Fatal("expected release, got nil")
	}
	if release.TagName != "v1.22.0" {
		t.Errorf("tag = %q, want %q", release.TagName, "v1.22.0")
	}
	if release.Name != "Go 1.22" {
		t.Errorf("name = %q, want %q", release.Name, "Go 1.22")
	}
}

func TestGetLatestRelease_NoReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient("")
	c.baseURL = srv.URL

	release, err := c.GetLatestRelease(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release != nil {
		t.Errorf("expected nil release, got %+v", release)
	}
}

func TestDoRequest_RateLimitRetry_RetryAfterHeader(t *testing.T) {
	var attempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count <= 2 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": 1}`)) //nolint:errcheck // test: write ignored
	}))
	defer srv.Close()

	c := NewClient("")
	c.baseURL = srv.URL

	exists, err := c.RepoExists(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected repo to exist after retry")
	}

	got := atomic.LoadInt32(&attempts)
	if got != 3 {
		t.Errorf("attempts = %d, want 3 (2 retries + 1 success)", got)
	}
}

func TestDoRequest_RateLimitRetry_XRateLimitResetHeader(t *testing.T) {
	var attempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count == 1 {
			resetTime := time.Now().Add(1 * time.Second).Unix()
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetTime))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": 1}`)) //nolint:errcheck // test: write ignored
	}))
	defer srv.Close()

	c := NewClient("")
	c.baseURL = srv.URL

	exists, err := c.RepoExists(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected repo to exist after retry")
	}
}

func TestDoRequest_RateLimitExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient("")
	c.baseURL = srv.URL

	_, err := c.RepoExists(context.Background(), "owner", "repo")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	expected := "GitHub rate limit exceeded after 3 retries"
	if err.Error() != expected {
		t.Errorf("error = %q, want %q", err.Error(), expected)
	}
}

func TestDoRequest_ContextCancelled_DuringRetry(t *testing.T) {
	var attempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient("")
	c.baseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := c.RepoExists(ctx, "owner", "repo")
	if err == nil {
		t.Fatal("expected error from canceled context")
	}

	got := atomic.LoadInt32(&attempts)
	if got > 2 {
		t.Errorf("attempts = %d, expected context to cancel before exhausting retries", got)
	}
}

func TestParseRateLimitWait_RetryAfterHeader(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "5")

	wait := HeaderAwareRetry{}.NextWait(h, 0)
	if wait != 5*time.Second {
		t.Errorf("wait = %v, want 5s", wait)
	}
}

func TestParseRateLimitWait_XRateLimitResetHeader(t *testing.T) {
	h := http.Header{}
	resetTime := time.Now().Add(10 * time.Second).Unix()
	h.Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetTime))

	wait := HeaderAwareRetry{}.NextWait(h, 0)
	if wait < 8*time.Second || wait > 12*time.Second {
		t.Errorf("wait = %v, want ~10s", wait)
	}
}

func TestParseRateLimitWait_NoHeaders_ExponentialBackoff(t *testing.T) {
	h := http.Header{}

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
	}

	for _, tt := range tests {
		got := HeaderAwareRetry{}.NextWait(h, tt.attempt)
		if got != tt.want {
			t.Errorf("attempt %d: wait = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestParseRateLimitWait_XRateLimitReset_TooFarInFuture(t *testing.T) {
	h := http.Header{}
	resetTime := time.Now().Add(5 * time.Minute).Unix()
	h.Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetTime))

	wait := HeaderAwareRetry{}.NextWait(h, 1)
	// Should fall back to exponential backoff (2s for attempt 1) since 5min > 120s cap
	if wait != 2*time.Second {
		t.Errorf("wait = %v, want 2s (exponential backoff for attempt 1)", wait)
	}
}
