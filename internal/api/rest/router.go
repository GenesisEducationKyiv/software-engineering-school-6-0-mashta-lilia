package rest

import (
	"database/sql"
	"net/http"

	"github-release-notifier/internal/api/middleware"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func NewRouter(h *Handler, db *sql.DB, apiKey string, subscribeLimiter *middleware.RateLimiter, swaggerPath string) *chi.Mux {
	r := chi.NewRouter()

	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.RequestID)
	r.Use(middleware.Metrics)

	r.Get("/", root)
	r.Get("/health", healthCheck(db))
	r.Get("/swagger.yaml", serveFile(swaggerPath))
	r.Handle("/metrics", promhttp.Handler())

	r.With(subscribeLimiter.Limit).Post("/api/subscribe", h.Subscribe)
	r.Get("/api/confirm/{token}", h.Confirm)
	r.Get("/api/unsubscribe/{token}", h.Unsubscribe)
	r.With(middleware.APIKeyAuth(apiKey)).Get("/api/subscriptions", h.GetSubscriptions)

	return r
}

func root(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{
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

func healthCheck(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			respondJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
			return
		}
		respondJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
	}
}
