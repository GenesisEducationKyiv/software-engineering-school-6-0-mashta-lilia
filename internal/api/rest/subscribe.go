package rest

import (
	"encoding/json"
	"errors"
	"github-release-notifier/internal/service"
	"net/http"
)

type subscribeRequest struct {
	Email string `json:"email"`
	Repo  string `json:"repo"`
}

func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	var req subscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := h.svc.Subscribe(r.Context(), req.Email, req.Repo)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidRepo):
			respondError(w, http.StatusBadRequest, "invalid repository format, expected owner/repo")
		case errors.Is(err, service.ErrInvalidEmail):
			respondError(w, http.StatusBadRequest, "invalid email address")
		case errors.Is(err, service.ErrRepoNotFound):
			respondError(w, http.StatusNotFound, "repository not found on GitHub")
		case errors.Is(err, service.ErrAlreadyExists):
			respondError(w, http.StatusConflict, "subscription already exists")
		case errors.Is(err, service.ErrEmailSendFailed):
			respondError(w, http.StatusServiceUnavailable,
				"failed to send confirmation email, please try again")
		default:
			respondError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "Subscription created. Please confirm via email.",
	})
}
