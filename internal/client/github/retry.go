package github

import (
	"net/http"
	"strconv"
	"time"
)

const maxBackoffBitShift = 62

// HeaderAwareRetry decides how long to wait before retrying a 429 response:
// Retry-After first, then X-RateLimit-Reset (capped at 120s for clock-skew
// safety), then exponential backoff.
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
