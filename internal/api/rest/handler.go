package rest

import (
	"context"
	"encoding/json"
	"github-release-notifier/internal/model"
	"log/slog"
	"net/http"
)

// SubscriptionUseCase is the contract Handler depends on. Defining it here
// (the consumer side) instead of typing svc as *service.SubscriptionService
// inverts the dependency: the HTTP layer no longer knows about the concrete
// service struct, and tests can substitute a small fake without standing up
// the full service graph.
type SubscriptionUseCase interface {
	Subscribe(ctx context.Context, email, repo string) error
	Confirm(ctx context.Context, token string) error
	Unsubscribe(ctx context.Context, token string) error
	GetSubscriptions(ctx context.Context, email string) ([]model.Subscription, error)
}

type Handler struct {
	svc SubscriptionUseCase
}

func NewHandler(svc SubscriptionUseCase) *Handler {
	return &Handler{svc: svc}
}

type errorResponse struct {
	Error string `json:"error"`
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, errorResponse{Error: msg})
}
