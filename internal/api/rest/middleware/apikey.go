package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
)

// APIKeyAuth enforces X-API-Key. An empty apiKey disables auth entirely —
// the composition root is expected to warn on that path. The bypass decision
// is resolved at construction time so it costs nothing per request.
func APIKeyAuth(apiKey string) func(http.Handler) http.Handler {
	if apiKey == "" {
		return func(next http.Handler) http.Handler { return next }
	}

	// Compare fixed-length sha256 digests, not raw keys: subtle.Constant
	// TimeCompare short-circuits on length mismatch, leaking key length.
	expected := sha256.Sum256([]byte(apiKey))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := sha256.Sum256([]byte(r.Header.Get("X-API-Key")))
			if subtle.ConstantTimeCompare(got[:], expected[:]) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				if err := json.NewEncoder(w).Encode(
					map[string]string{"error": "invalid or missing API key"},
				); err != nil {
					slog.Error("Failed to encode auth error response", "err", err)
				}
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
