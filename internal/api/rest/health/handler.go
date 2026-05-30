package health

import (
	"context"
	"encoding/json"
	"net/http"

	"github-release-notifier/internal/platform/logger"
)

type checker interface {
	Check(ctx context.Context) error
}

func Handler(c checker, logs ...logger.Logger) http.HandlerFunc {
	log := optionalLogger(logs...)
	if c == nil {
		return func(w http.ResponseWriter, r *http.Request) {
			respond(r.Context(), log, w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if err := c.Check(r.Context()); err != nil {
			respond(r.Context(), log, w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
			return
		}
		respond(r.Context(), log, w, http.StatusOK, map[string]string{"status": "healthy"})
	}
}

func respond(ctx context.Context, log logger.Logger, w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Error(ctx, "health_response_encode_failed", "err", err)
	}
}

func optionalLogger(logs ...logger.Logger) logger.Logger {
	if len(logs) > 0 && logs[0] != nil {
		return logs[0]
	}
	return logger.Nop()
}
