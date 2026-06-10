package github

import (
	"net/http"
	"strconv"
	"time"
)

const (
	// Cap X-RateLimit-Reset so a minutes-long window or clock skew can't stall callers.
	maxResetWait      = 120 * time.Second
	maxBackoffAttempt = 2
)

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
