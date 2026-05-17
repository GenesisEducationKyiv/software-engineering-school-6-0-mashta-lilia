package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

type checker interface {
	Check(ctx context.Context) error
}

func Handler(c checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := c.Check(r.Context()); err != nil {
			respond(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
			return
		}
		respond(w, http.StatusOK, map[string]string{"status": "healthy"})
	}
}

func respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("Failed to encode health response", "err", err)
	}
}
