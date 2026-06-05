package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"github-release-notifier/internal/platform/logger"
	"net/http"
)

// Empty apiKey fails closed; never silently bypass auth on a PII endpoint.
func APIKeyAuth(apiKey string, log *logger.Logger) func(http.Handler) http.Handler {
	if log == nil {
		log = logger.Nop()
	}
	if apiKey == "" {
		// Surface the misconfiguration in the logs; otherwise it only shows up as
		// a confusing 401 in client reports.
		log.Warn(context.Background(), "api_key_auth_not_configured",
			"detail", "X-API-Key auth is enabled but no API key is set; all requests will be rejected")
		return func(_ http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeAuthError(r.Context(), log, w, "API key authentication is not configured")
			})
		}
	}

	// Compare fixed-length digests so ConstantTimeCompare doesn't leak length via early return.
	expected := sha256.Sum256([]byte(apiKey))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := sha256.Sum256([]byte(r.Header.Get("X-API-Key")))
			if subtle.ConstantTimeCompare(got[:], expected[:]) != 1 {
				writeAuthError(r.Context(), log, w, "invalid or missing API key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeAuthError(ctx context.Context, log *logger.Logger, w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		log.Error(ctx, "auth_error_encode_failed", "err", err)
	}
}
