package middleware

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

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
				"path", redactPath(r.URL),
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
			)
		}()
		next.ServeHTTP(ww, r)
	})
}

func redactPath(u *url.URL) string {
	path := u.Path
	switch {
	case strings.HasPrefix(path, "/api/confirm/"):
		path = "/api/confirm/<redacted>"
	case strings.HasPrefix(path, "/api/unsubscribe/"):
		path = "/api/unsubscribe/<redacted>"
	}
	q := u.Query()
	if q.Has("email") {
		q.Set("email", "<redacted>")
	}
	if encoded := q.Encode(); encoded != "" {
		return path + "?" + encoded
	}
	return path
}
