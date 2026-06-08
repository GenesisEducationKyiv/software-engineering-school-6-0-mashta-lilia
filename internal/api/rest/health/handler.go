package health

import (
	"context"
	"encoding/json"
	"github-release-notifier/internal/platform/logger"
	"net/http"
)

type checker interface {
	Check(ctx context.Context) error
}

func Handler(c checker, log *logger.Logger) http.HandlerFunc {
	if log == nil {
		log = logger.Nop()
	}
	if c == nil {
		return func(w http.ResponseWriter, r *http.Request) {
			respond(
				r.Context(), log, w, http.StatusServiceUnavailable,
				map[string]string{"status": "unhealthy"},
			)
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if err := c.Check(r.Context()); err != nil {
			respond(
				r.Context(), log, w, http.StatusServiceUnavailable,
				map[string]string{"status": "unhealthy"},
			)
			return
		}
		respond(r.Context(), log, w, http.StatusOK, map[string]string{"status": "healthy"})
	}
}

func respond(ctx context.Context, log *logger.Logger, w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Error(ctx, "health_response_encode_failed", "err", err)
	}
}
