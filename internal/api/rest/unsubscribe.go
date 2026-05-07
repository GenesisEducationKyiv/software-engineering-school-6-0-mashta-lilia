package rest

import (
	"errors"
	"github-release-notifier/internal/service"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		respondError(w, http.StatusBadRequest, "token is required")
		return
	}

	err := h.svc.Unsubscribe(r.Context(), token)
	if err != nil {
		if errors.Is(err, service.ErrTokenNotFound) {
			respondError(w, http.StatusNotFound, "invalid or expired token")
			return
		}
		respondError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "Successfully unsubscribed.",
	})
}
