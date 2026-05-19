package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// AccessLog logs each request with method, redacted URL, status, and duration.
// Tokens embedded in /api/confirm/<token> and /api/unsubscribe/<token> are
// bearer credentials; the email query param on /api/subscriptions is PII.
// Logging them raw lets anyone with log access act on behalf of users.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		defer func() {
			slog.Info("http",
				"method", r.Method,
				"path", redactPath(r),
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
			)
		}()
		next.ServeHTTP(ww, r)
	})
}

// redactPath substitutes the chi route pattern (e.g. /api/confirm/{token})
// for the raw path, so route params holding bearer tokens never reach logs.
// Query params with PII (email) are masked separately.
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
