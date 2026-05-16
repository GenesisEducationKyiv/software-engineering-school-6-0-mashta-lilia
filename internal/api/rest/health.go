package rest

import (
	"context"
	"net/http"
)

// HealthChecker reports the liveness of a downstream dependency. Defined
// on the consumer side (Go idiom) — same convention as SubscriptionUseCase.
// The router declares what it needs; any implementation that fits is OK.
type HealthChecker interface {
	Check(ctx context.Context) error
}

func healthHandler(health HealthChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := health.Check(r.Context()); err != nil {
			respondJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
			return
		}
		respondJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
	}
}
