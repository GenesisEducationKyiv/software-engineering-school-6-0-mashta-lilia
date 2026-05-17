package rest

import (
	"context"
	"encoding/json"
	"github-release-notifier/internal/api/rest/health"
	"github-release-notifier/internal/api/rest/middleware"
	"github-release-notifier/internal/api/rest/subscription"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type HealthChecker interface {
	Check(ctx context.Context) error
}

func NewRouter(
	h *subscription.Handler,
	hc HealthChecker,
	apiKey string,
	subscribeLimiter *middleware.RateLimiter,
	swaggerPath string,
) *chi.Mux {
	r := chi.NewRouter()

	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.RequestID)
	r.Use(middleware.Metrics)

	r.Get("/", root)
	r.Get("/health", health.Handler(hc))
	r.Get("/swagger.yaml", serveFile(swaggerPath))
	r.Handle("/metrics", promhttp.Handler())

	r.With(subscribeLimiter.Limit).Post("/api/subscribe", h.Subscribe)
	r.Get("/api/confirm/{token}", h.Confirm)
	r.Get("/api/unsubscribe/{token}", h.Unsubscribe)
	r.With(middleware.APIKeyAuth(apiKey)).Get("/api/subscriptions", h.List)

	return r
}

func root(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"service": "GitHub Release Notification API",
		"docs":    "/swagger.yaml",
		"health":  "/health",
		"metrics": "/metrics",
	})
}

func serveFile(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("Failed to encode response", "err", err)
	}
}
