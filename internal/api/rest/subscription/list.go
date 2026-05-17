package subscription

import "net/http"

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		respondError(w, http.StatusBadRequest, "email query parameter is required")
		return
	}

	subs, err := h.svc.GetSubscriptions(r.Context(), email)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, subs)
}
