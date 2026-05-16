package github

import (
	"net/http"
	"strconv"
	"time"
)

const maxBackoffBitShift = 62

// RetryStrategy decides how long the client should wait before retrying a
// 429 Too Many Requests response. Extracting this as an interface means the
// retry policy is open for extension (jitter, circuit breaker, custom
// upstream caps) without modifying Client itself — Open/Closed in practice.
type RetryStrategy interface {
	NextWait(headers http.Header, attempt int) time.Duration
}

// HeaderAwareRetry is the default GitHub-aware strategy:
//  1. Retry-After header (seconds) — explicit instruction from the server.
//  2. X-RateLimit-Reset header (Unix timestamp) — capped at 120s to avoid
//     long stalls from clock skew.
//  3. Exponential backoff (1s, 2s, 4s, …) — fallback when no headers help.
type HeaderAwareRetry struct{}

func (HeaderAwareRetry) NextWait(headers http.Header, attempt int) time.Duration {
	if ra := headers.Get("Retry-After"); ra != "" {
		if seconds, err := strconv.Atoi(ra); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}

	if reset := headers.Get("X-RateLimit-Reset"); reset != "" {
		if ts, err := strconv.ParseInt(reset, 10, 64); err == nil {
			wait := time.Until(time.Unix(ts, 0))
			if wait > 0 && wait < 120*time.Second {
				return wait
			}
		}
	}

	return time.Duration(1<<min(attempt, maxBackoffBitShift)) * time.Second
}
