package subscription

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		h.respondError(r.Context(), w, http.StatusBadRequest, "token is required")
		return
	}

	if err := h.svc.Unsubscribe(r.Context(), token); err != nil {
		h.writeServiceError(r.Context(), w, err)
		return
	}

	h.respondJSON(r.Context(), w, http.StatusOK, map[string]string{
		"message": "Successfully unsubscribed.",
	})
}
