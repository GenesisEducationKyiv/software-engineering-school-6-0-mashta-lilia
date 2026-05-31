package subscription

import (
	"bytes"
	"context"
	"encoding/json"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/subscription"
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
	log logger.Logger
}

func NewHandler(svc subscriptionService, logs ...logger.Logger) *Handler {
	return &Handler{svc: svc, log: logger.Or(logs...)}
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *Handler) respondJSON(ctx context.Context, w http.ResponseWriter, status int, data any) {
	// Buffer first so an encode failure can still flip the status to 500.
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(data); err != nil {
		h.log.Error(ctx, "response_encode_failed", "err", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if _, writeErr := w.Write([]byte(`{"error":"internal server error"}` + "\n")); writeErr != nil {
			h.log.Error(ctx, "fallback_response_write_failed", "err", writeErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		h.log.Error(ctx, "response_write_failed", "err", err)
	}
}

func (h *Handler) respondError(ctx context.Context, w http.ResponseWriter, status int, msg string) {
	h.respondJSON(ctx, w, status, errorResponse{Error: msg})
}
