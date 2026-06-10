package subscription

import "net/http"

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		h.respondError(r.Context(), w, http.StatusBadRequest, "email query parameter is required")
		return
	}

	subs, err := h.svc.GetSubscriptions(r.Context(), email)
	if err != nil {
		h.writeServiceError(r.Context(), w, err)
		return
	}

	h.respondJSON(r.Context(), w, http.StatusOK, subs)
}
