package subscription

import (
	"bytes"
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
	// Encode into a buffer first: if marshaling fails after WriteHeader,
	// the client gets a success status with a truncated body. Buffering
	// lets us downgrade the status to 500 on encode failure.
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(data); err != nil {
		slog.Error("Failed to encode response", "err", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if _, writeErr := w.Write([]byte(`{"error":"internal server error"}` + "\n")); writeErr != nil {
			slog.Error("Failed to write fallback response", "err", writeErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		slog.Error("Failed to write response", "err", err)
	}
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, errorResponse{Error: msg})
}
