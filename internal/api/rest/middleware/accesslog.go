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

func AccessLog(log logger.Logger) func(http.Handler) http.Handler {
	if log == nil {
		log = logger.Nop()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			defer func(ctx context.Context) {
				log.Info(ctx, "http_request",
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

func sanitizeHeaderValue(value string) string {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "authorization") || strings.Contains(lower, "bearer ") {
		if redacted, ok := logger.Redact("authorization", value).(string); ok {
			return redacted
		}
	}
	return value
}
