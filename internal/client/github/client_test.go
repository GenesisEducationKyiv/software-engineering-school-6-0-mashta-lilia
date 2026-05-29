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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoExists_Found(t *testing.T) {
	t.Parallel()
	var gotPath, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAccept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": 1}`)) //nolint:errcheck // test: write ignored
	}))
	defer srv.Close()

	c := NewClient("")
	c.baseURL = srv.URL

	exists, err := c.RepoExists(context.Background(), "golang", "go")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, "/repos/golang/go", gotPath)
	assert.Equal(t, "application/vnd.github.v3+json", gotAccept)
}

func TestRepoExists_NotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient("")
	c.baseURL = srv.URL

	exists, err := c.RepoExists(context.Background(), "nonexistent", "repo")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestRepoExists_WithToken(t *testing.T) {
	t.Parallel()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient("ghp_testtoken123")
	c.baseURL = srv.URL

	_, err := c.RepoExists(context.Background(), "owner", "repo")
	require.NoError(t, err)
	assert.Equal(t, "Bearer ghp_testtoken123", gotAuth)
}

func TestGetLatestRelease_Success(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tag_name":"v1.22.0","name":"Go 1.22","html_url":"https://github.com/golang/go/releases/tag/v1.22.0"}`)) //nolint:errcheck,revive // test: write ignored, URL exceeds limit
	}))
	defer srv.Close()

	c := NewClient("")
	c.baseURL = srv.URL

	release, err := c.GetLatestRelease(context.Background(), "golang", "go")
	require.NoError(t, err)
	require.NotNil(t, release)
	assert.Equal(t, "/repos/golang/go/releases/latest", gotPath)
	assert.Equal(t, "v1.22.0", release.TagName)
	assert.Equal(t, "Go 1.22", release.Name)
}

func TestGetLatestRelease_NoReleases(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient("")
	c.baseURL = srv.URL

	release, err := c.GetLatestRelease(context.Background(), "owner", "repo")
	require.NoError(t, err)
	assert.Nil(t, release)
}

func TestDoRequest_RateLimitRetry_RetryAfterHeader(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts), "expected 2 retries + 1 success")
}

func TestDoRequest_RateLimitRetry_XRateLimitResetHeader(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestDoRequest_RateLimitExhausted(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient("")
	c.baseURL = srv.URL

	_, err := c.RepoExists(context.Background(), "owner", "repo")
	require.Error(t, err)
	assert.EqualError(t, err, "GitHub rate limit exceeded after 3 retries")
}

func TestDoRequest_ContextCancelled_DuringRetry(t *testing.T) {
	t.Parallel()
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
	require.Error(t, err)
	assert.LessOrEqual(t, atomic.LoadInt32(&attempts), int32(2),
		"context should cancel before exhausting retries")
}

func TestParseRateLimitWait_RetryAfterHeader(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	h.Set("Retry-After", "5")

	wait := HeaderAwareRetry{}.NextWait(h, 0)
	assert.Equal(t, 5*time.Second, wait)
}

func TestParseRateLimitWait_XRateLimitResetHeader(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	resetTime := time.Now().Add(10 * time.Second).Unix()
	h.Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetTime))

	wait := HeaderAwareRetry{}.NextWait(h, 0)
	assert.GreaterOrEqual(t, wait, 8*time.Second)
	assert.LessOrEqual(t, wait, 12*time.Second)
}

func TestParseRateLimitWait_NoHeaders_ExponentialBackoff(t *testing.T) {
	t.Parallel()
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
		t.Run(fmt.Sprintf("attempt=%d", tt.attempt), func(t *testing.T) {
			assert.Equal(t, tt.want, HeaderAwareRetry{}.NextWait(h, tt.attempt))
		})
	}
}

func TestParseRateLimitWait_XRateLimitReset_TooFarInFuture(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	resetTime := time.Now().Add(5 * time.Minute).Unix()
	h.Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetTime))

	wait := HeaderAwareRetry{}.NextWait(h, 1)
	assert.Equal(t, maxResetWait, wait)
}

func TestParseRateLimitWait_ExponentialBackoff_BoundedAtMax(t *testing.T) {
	t.Parallel()
	h := http.Header{}

	for _, attempt := range []int{3, 5, 10, 62} {
		t.Run(fmt.Sprintf("attempt=%d", attempt), func(t *testing.T) {
			assert.Equal(t, 4*time.Second, HeaderAwareRetry{}.NextWait(h, attempt))
		})
	}
}

func TestParseRateLimitWait_NegativeAttempt_TreatsAsZero(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	assert.Equal(t, 1*time.Second, HeaderAwareRetry{}.NextWait(h, -1))
}
