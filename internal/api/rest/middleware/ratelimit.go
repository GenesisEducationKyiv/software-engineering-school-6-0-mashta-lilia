package middleware

import (
	"encoding/json"
	"fmt"
	"github-release-notifier/internal/platform/logger"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const xForwardedForClientIPSplit = 2

type visitor struct {
	count   int
	resetAt time.Time
}

type RateLimiter struct {
	mu           sync.Mutex
	visitors     map[string]*visitor
	limit        int
	window       time.Duration
	done         chan struct{}
	stopOnce     sync.Once
	trustedProxy bool
	log          *logger.Logger
}

func NewRateLimiter(limit int, window time.Duration, trustedProxy bool, log *logger.Logger) *RateLimiter {
	if log == nil {
		log = logger.Nop()
	}
	rl := &RateLimiter{
		visitors:     make(map[string]*visitor),
		limit:        limit,
		window:       window,
		done:         make(chan struct{}),
		trustedProxy: trustedProxy,
		log:          log,
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() { close(rl.done) })
}

func (rl *RateLimiter) clientIP(r *http.Request) string {
	if rl.trustedProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			ip := strings.SplitN(forwarded, ",", xForwardedForClientIPSplit)[0]
			return strings.TrimSpace(ip)
		}
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			return strings.TrimSpace(realIP)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (rl *RateLimiter) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := rl.clientIP(r)

		rl.mu.Lock()
		v, exists := rl.visitors[ip]
		now := time.Now()

		if !exists || now.After(v.resetAt) {
			rl.visitors[ip] = &visitor{count: 1, resetAt: now.Add(rl.window)}
			rl.mu.Unlock()
			next.ServeHTTP(w, r)
			return
		}

		if v.count >= rl.limit {
			retryAfter := int(time.Until(v.resetAt).Seconds()) + 1
			rl.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			w.WriteHeader(http.StatusTooManyRequests)
			if err := json.NewEncoder(w).Encode(
				map[string]string{"error": "rate limit exceeded"},
			); err != nil {
				rl.log.Error(r.Context(), "rate_limit_response_encode_failed", "err", err)
			}
			return
		}

		v.count++
		rl.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-rl.done:
			return
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for ip, v := range rl.visitors {
				if now.After(v.resetAt) {
					delete(rl.visitors, ip)
				}
			}
			rl.mu.Unlock()
		}
	}
}
