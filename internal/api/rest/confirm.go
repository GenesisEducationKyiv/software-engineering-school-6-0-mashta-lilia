package rest

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github-release-notifier/internal/service"
)

func (h *Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		respondError(w, http.StatusBadRequest, "token is required")
		return
	}

	err := h.svc.Confirm(r.Context(), token)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTokenNotFound),
			errors.Is(err, service.ErrSubscriptionInactive):
			respondError(w, http.StatusNotFound, "invalid or expired token")
		default:
			respondError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "Subscription confirmed successfully.",
	})
}
