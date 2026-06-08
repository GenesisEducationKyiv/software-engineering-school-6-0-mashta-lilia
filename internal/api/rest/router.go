package rest

import (
	"context"
	"encoding/json"
	"github-release-notifier/internal/api/rest/health"
	"github-release-notifier/internal/api/rest/middleware"
	"github-release-notifier/internal/api/rest/subscription"
	"github-release-notifier/internal/platform/logger"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type healthChecker interface {
	Check(ctx context.Context) error
}

func NewRouter(
	h *subscription.Handler,
	hc healthChecker,
	apiKey string,
	subscribeLimiter *middleware.RateLimiter,
	swaggerPath string,
	log *logger.Logger,
) *chi.Mux {
	if log == nil {
		log = logger.Nop()
	}
	r := chi.NewRouter()

	r.Use(middleware.TraceID)
	r.Use(middleware.AccessLog(log))
	r.Use(chimw.Recoverer)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.Metrics)

	r.Get("/", root(log))
	r.Get("/health", health.Handler(hc, log))
	r.Get("/swagger.yaml", serveFile(swaggerPath))
	r.Handle("/metrics", promhttp.Handler())

	r.With(subscribeLimiter.Limit).Post("/api/subscribe", h.Subscribe)
	r.Get("/api/confirm/{token}", h.Confirm)
	r.Get("/api/unsubscribe/{token}", h.Unsubscribe)
	r.With(middleware.APIKeyAuth(apiKey, log)).Get("/api/subscriptions", h.List)

	return r
}

func root(log *logger.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(r.Context(), log, w, http.StatusOK, map[string]string{
			"service": "GitHub Release Notification API",
			"docs":    "/swagger.yaml",
			"health":  "/health",
			"metrics": "/metrics",
		})
	}
}

func serveFile(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	}
}

func writeJSON(ctx context.Context, log *logger.Logger, w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Error(ctx, "response_encode_failed", "err", err)
	}
}
