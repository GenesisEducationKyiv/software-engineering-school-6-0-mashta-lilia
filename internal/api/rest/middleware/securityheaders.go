package middleware

import (
	"net/http"
	"strings"
)

// SecurityHeaders sets defensive response headers on every response.
// Cache-Control: no-store is applied only under /api/* so confirmation
// tokens and PII responses are not cached by intermediaries, while
// /swagger.yaml, /metrics, /health, and / can still be cached normally.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
