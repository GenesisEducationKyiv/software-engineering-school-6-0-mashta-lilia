// Package resthttp serves the notification REST API kept alongside gRPC for the
// REST-vs-gRPC comparison (HW10). POST /api/v1/verify-email is the HTTP/JSON
// counterpart of the VerifyEmail gRPC RPC; both reuse Service.SendConfirmation.
package resthttp

import (
	"context"
	"encoding/json"
	"errors"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/services/notification"
	"net/http"
)

const maxBodyBytes = 1 << 20

type confirmationSender interface {
	SendConfirmation(ctx context.Context, confirmation notification.Confirmation) (bool, error)
}

type Handler struct {
	sender confirmationSender
	log    *logger.Logger
}

func NewHandler(sender confirmationSender, log *logger.Logger) (*Handler, error) {
	if sender == nil {
		return nil, errors.New("notification rest: sender is nil")
	}
	if log == nil {
		log = logger.Nop()
	}
	return &Handler{sender: sender, log: log}, nil
}

type verifyEmailRequest struct {
	Email      string `json:"email"`
	ConfirmURL string `json:"confirm_url"`
	Repo       string `json:"repo"`
}

type verifyEmailResponse struct {
	Delivered bool `json:"delivered"`
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/verify-email", h.verifyEmail)
	return mux
}

func (h *Handler) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var req verifyEmailRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		h.writeError(r.Context(), w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" || req.ConfirmURL == "" || req.Repo == "" {
		h.writeError(r.Context(), w, http.StatusBadRequest, "email, confirm_url and repo are required")
		return
	}
	delivered, err := h.sender.SendConfirmation(r.Context(), notification.Confirmation{
		Email:      req.Email,
		ConfirmURL: req.ConfirmURL,
		Repo:       req.Repo,
	})
	if err != nil {
		h.log.Error(r.Context(), "verify_email_failed", "err", err)
		h.writeError(r.Context(), w, http.StatusInternalServerError, "failed to send verification email")
		return
	}
	h.writeJSON(r.Context(), w, http.StatusOK, verifyEmailResponse{Delivered: delivered})
}

func (h *Handler) writeJSON(ctx context.Context, w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.log.Error(ctx, "rest_encode_failed", "err", err)
	}
}

func (h *Handler) writeError(ctx context.Context, w http.ResponseWriter, status int, msg string) {
	h.writeJSON(ctx, w, status, map[string]string{"error": msg})
}
