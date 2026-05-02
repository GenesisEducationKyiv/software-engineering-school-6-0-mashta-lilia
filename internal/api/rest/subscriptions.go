package rest

import (
	"errors"
	"net/http"

	"github-release-notifier/internal/service"
)

func (h *Handler) GetSubscriptions(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		respondError(w, http.StatusBadRequest, "email query parameter is required")
		return
	}

	subs, err := h.svc.GetSubscriptions(r.Context(), email)
	if err != nil {
		if errors.Is(err, service.ErrInvalidEmail) {
			respondError(w, http.StatusBadRequest, "invalid email address")
			return
		}
		respondError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	respondJSON(w, http.StatusOK, subs)
}
