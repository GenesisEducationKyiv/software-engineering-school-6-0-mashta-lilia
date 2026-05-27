package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github-release-notifier/internal/api/rest/middleware"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLimiter(t *testing.T, limit int, window time.Duration, trustProxy bool) *middleware.RateLimiter {
	t.Helper()
	rl := middleware.NewRateLimiter(limit, window, trustProxy)
	t.Cleanup(rl.Stop)
	return rl
}

func dispatchN(rl *middleware.RateLimiter, n int, prepare func(*http.Request)) []int {
	h := rl.Limit(okHandler())
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/subscribe", http.NoBody)
		req.RemoteAddr = "10.0.0.1:1234"
		if prepare != nil {
			prepare(req)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		codes[i] = rec.Code
	}
	return codes
}

func TestRateLimiter_WithinLimit_AllPass(t *testing.T) {
	t.Parallel()
	rl := newLimiter(t, 3, time.Minute, false)
	codes := dispatchN(rl, 3, nil)
	for i, code := range codes {
		assert.Equal(t, http.StatusOK, code, "request %d should pass", i+1)
	}
}

func TestRateLimiter_OverLimit_Returns429(t *testing.T) {
	t.Parallel()
	rl := newLimiter(t, 2, time.Minute, false)
	codes := dispatchN(rl, 3, nil)
	assert.Equal(t, http.StatusOK, codes[0])
	assert.Equal(t, http.StatusOK, codes[1])
	assert.Equal(t, http.StatusTooManyRequests, codes[2])
}

func TestRateLimiter_429ResponseHasRetryAfterAndJSON(t *testing.T) {
	t.Parallel()
	rl := newLimiter(t, 1, 30*time.Second, false)
	h := rl.Limit(okHandler())

	// First request: OK.
	req1 := httptest.NewRequest(http.MethodPost, "/api/subscribe", http.NoBody)
	req1.RemoteAddr = "10.0.0.1:1"
	h.ServeHTTP(httptest.NewRecorder(), req1)

	// Second request: rate limited.
	req2 := httptest.NewRequest(http.MethodPost, "/api/subscribe", http.NoBody)
	req2.RemoteAddr = "10.0.0.1:1"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req2)

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	retryAfter, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, retryAfter, 1)
	assert.LessOrEqual(t, retryAfter, 31)

	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "rate limit exceeded", body["error"])
}

func TestRateLimiter_PerIPIsolation(t *testing.T) {
	t.Parallel()
	rl := newLimiter(t, 1, time.Minute, false)
	h := rl.Limit(okHandler())

	send := func(ip string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/subscribe", http.NoBody)
		req.RemoteAddr = ip
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	// Exhaust IP1's bucket so the test can distinguish per-IP isolation from
	// a no-op limiter: only a real per-IP bucket lets IP2 pass once IP1 is 429.
	assert.Equal(t, http.StatusOK, send("10.0.0.1:1"))
	assert.Equal(t, http.StatusTooManyRequests, send("10.0.0.1:1"))
	assert.Equal(t, http.StatusOK, send("10.0.0.2:1"),
		"IP2 must have its own bucket while IP1 is throttled")
}

func TestRateLimiter_WindowResets(t *testing.T) {
	t.Parallel()
	// Production code calls time.Now directly. Run inside a synctest bubble
	// so the rate-limiter's clock advances virtually — no wall-clock sleep,
	// no GC/scheduler-jitter flake budget to pad against.
	synctest.Test(t, func(t *testing.T) {
		const window = 200 * time.Millisecond

		rl := middleware.NewRateLimiter(1, window, false)
		defer rl.Stop()
		h := rl.Limit(okHandler())

		send := func() int {
			req := httptest.NewRequest(http.MethodPost, "/api/subscribe", http.NoBody)
			req.RemoteAddr = "10.0.0.1:1"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			return rec.Code
		}

		assert.Equal(t, http.StatusOK, send())
		assert.Equal(t, http.StatusTooManyRequests, send())
		time.Sleep(window + time.Second)
		assert.Equal(t, http.StatusOK, send(), "after window expires, requests should pass again")
	})
}

func TestRateLimiter_TrustedProxy_HonorsXForwardedFor(t *testing.T) {
	t.Parallel()
	rl := newLimiter(t, 1, time.Minute, true)
	h := rl.Limit(okHandler())

	// Two requests from the same RemoteAddr but different X-Forwarded-For
	// should *not* share a bucket when proxy headers are trusted.
	for _, fwd := range []string{"203.0.113.1", "203.0.113.2"} {
		req := httptest.NewRequest(http.MethodPost, "/api/subscribe", http.NoBody)
		req.RemoteAddr = "10.0.0.99:1"
		req.Header.Set("X-Forwarded-For", fwd+", 10.0.0.1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "first request for forwarded=%s should pass", fwd)
	}
}

func TestRateLimiter_UntrustedProxy_IgnoresXForwardedFor(t *testing.T) {
	t.Parallel()
	rl := newLimiter(t, 1, time.Minute, false)
	h := rl.Limit(okHandler())

	// Different X-Forwarded-For but same RemoteAddr; second should be 429
	// because untrusted mode uses RemoteAddr only.
	for i, fwd := range []string{"203.0.113.1", "203.0.113.2"} {
		req := httptest.NewRequest(http.MethodPost, "/api/subscribe", http.NoBody)
		req.RemoteAddr = "10.0.0.99:1"
		req.Header.Set("X-Forwarded-For", fwd)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if i == 0 {
			assert.Equal(t, http.StatusOK, rec.Code)
		} else {
			assert.Equal(t, http.StatusTooManyRequests, rec.Code,
				"untrusted-proxy mode should rate-limit by RemoteAddr regardless of X-Forwarded-For")
		}
	}
}

func TestRateLimiter_Stop_IsIdempotent(t *testing.T) {
	t.Parallel()
	rl := middleware.NewRateLimiter(1, time.Minute, false)
	rl.Stop()
	assert.NotPanics(t, rl.Stop, "Stop must be safe to call twice")
}
