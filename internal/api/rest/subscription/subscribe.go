package subscription

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const maxSubscribeBodyBytes = 1 << 20 // 1 MiB cap; body only needs email + repo

type subscribeRequest struct {
	Email string `json:"email"`
	Repo  string `json:"repo"`
}

func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSubscribeBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req subscribeRequest
	if err := dec.Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var extra interface{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.svc.Subscribe(r.Context(), req.Email, req.Repo); err != nil {
		writeServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "Subscription created. Please confirm via email.",
	})
}
