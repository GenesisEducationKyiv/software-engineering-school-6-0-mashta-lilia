package github

import (
	"net/http"
	"strconv"
	"time"
)

const (
	// X-RateLimit-Reset is honored up to this cap to bound wait time even
	// when GitHub reports a window of minutes (clock skew or genuinely
	// long quota window) — 120s still respects the server's signal while
	// keeping caller latency tolerable.
	maxResetWait = 120 * time.Second

	// Bounds the exponential fallback to the 3-tier sequence 1s/2s/4s.
	maxBackoffAttempt = 2
)

// HeaderAwareRetry decides how long to wait before retrying a 429 response:
// Retry-After first, then X-RateLimit-Reset (capped at maxResetWait), then
// exponential backoff bounded to 1s/2s/4s.
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
			if wait > 0 {
				if wait > maxResetWait {
					return maxResetWait
				}
				return wait
			}
		}
	}

	if attempt < 0 {
		attempt = 0
	}
	return time.Duration(1<<min(attempt, maxBackoffAttempt)) * time.Second
}
