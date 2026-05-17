package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
)

// APIKeyAuth enforces X-API-Key. An empty apiKey is a misconfiguration:
// the middleware fails closed (always 401) rather than silently bypassing
// auth on a PII endpoint. The composition root is expected to warn loudly.
// The empty-key decision is resolved at construction time, not per request.
func APIKeyAuth(apiKey string) func(http.Handler) http.Handler {
	if apiKey == "" {
		return func(_ http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeAuthError(w, "API key authentication is not configured")
			})
		}
	}

	// Compare fixed-length sha256 digests, not raw keys: subtle.Constant
	// TimeCompare short-circuits on length mismatch, leaking key length.
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
