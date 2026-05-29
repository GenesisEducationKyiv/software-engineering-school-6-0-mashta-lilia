package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
)

// Empty apiKey fails closed; never silently bypass auth on a PII endpoint.
func APIKeyAuth(apiKey string) func(http.Handler) http.Handler {
	if apiKey == "" {
		return func(_ http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeAuthError(w, "API key authentication is not configured")
			})
		}
	}

	// Compare fixed-length digests so ConstantTimeCompare doesn't leak length via early return.
	expected := sha256.Sum256([]byte(apiKey))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := sha256.Sum256([]byte(r.Header.Get("X-API-Key")))
			if subtle.ConstantTimeCompare(got[:], expected[:]) != 1 {
				writeAuthError(w, "invalid or missing API key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Error("Failed to encode auth error response", "err", err)
	}
}
