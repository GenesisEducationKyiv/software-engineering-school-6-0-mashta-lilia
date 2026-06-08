package middleware

import (
	"context"
	"github-release-notifier/internal/platform/logger"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func AccessLog(l *logger.Logger) func(http.Handler) http.Handler {
	if l == nil {
		l = logger.Nop()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			defer func(ctx context.Context) {
				l.Info(ctx, "http_request",
					"method", r.Method,
					"route", routePattern(r),
					"status", ww.Status(),
					"bytes", ww.BytesWritten(),
					"duration_ms", time.Since(start).Milliseconds(),
					"remote_ip", remoteIP(r.RemoteAddr),
					"user_agent", sanitizeHeaderValue(r.UserAgent()),
				)
			}(r.Context())
			next.ServeHTTP(ww, r)
		})
	}
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if pattern := rc.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return "unmatched_path"
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// redactedValue replaces header values that look like they carry credentials.
const redactedValue = "<redacted>"

// sanitizeHeaderValue guards against a header (e.g. User-Agent) smuggling
// credentials into the logs. Redaction lives here, with the consumer, rather
// than in the logger.
func sanitizeHeaderValue(value string) string {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "authorization") || strings.Contains(lower, "bearer ") {
		return redactedValue
	}
	return value
}
