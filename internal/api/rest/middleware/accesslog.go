package middleware

import (
	"context"
	"github-release-notifier/internal/platform/logger"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Tokens in path and the email query param are credentials/PII; never log them raw.
func AccessLog(log logger.Logger) func(http.Handler) http.Handler {
	if log == nil {
		log = logger.New(logger.Config{Level: "info"})
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			defer func(ctx context.Context) {
				log.Info(ctx, "http",
					"method", r.Method,
					"path", redactPath(r),
					"status", ww.Status(),
					"bytes", ww.BytesWritten(),
					"duration_ms", time.Since(start).Milliseconds(),
					"remote", r.RemoteAddr,
				)
			}(r.Context())
			next.ServeHTTP(ww, r)
		})
	}
}

func redactPath(r *http.Request) string {
	path := r.URL.Path
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if pattern := rc.RoutePattern(); pattern != "" {
			path = pattern
		}
	}
	q := r.URL.Query()
	if q.Has("email") {
		q.Set("email", "<redacted>")
	}
	if encoded := q.Encode(); encoded != "" {
		return path + "?" + encoded
	}
	return path
}
