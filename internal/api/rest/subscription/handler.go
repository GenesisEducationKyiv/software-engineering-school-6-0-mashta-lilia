package subscription

import (
	"context"
	"encoding/json"
	"github-release-notifier/internal/subscription"
	"log/slog"
	"net/http"
)

type subscriptionService interface {
	Subscribe(ctx context.Context, email, repo string) error
	Confirm(ctx context.Context, token string) error
	Unsubscribe(ctx context.Context, token string) error
	GetSubscriptions(ctx context.Context, email string) ([]subscription.Subscription, error)
}

type Handler struct {
	svc subscriptionService
}

func NewHandler(svc subscriptionService) *Handler {
	return &Handler{svc: svc}
}

type errorResponse struct {
	Error string `json:"error"`
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("Failed to encode response", "err", err)
	}
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, errorResponse{Error: msg})
}
