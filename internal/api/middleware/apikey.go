package middleware

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
)

func APIKeyAuth(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiKey == "" {
				next.ServeHTTP(w, r)
				return
			}

			key := r.Header.Get("X-API-Key")
			if subtle.ConstantTimeCompare([]byte(key), []byte(apiKey)) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				if err := json.NewEncoder(w).Encode(
					map[string]string{"error": "invalid or missing API key"},
				); err != nil {
					slog.Error("failed to encode auth error response", "error", err)
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
