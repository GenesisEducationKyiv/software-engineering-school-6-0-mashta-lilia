package subscription

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github-release-notifier/internal/platform/logger"
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
		h.log.Error(r.Context(), "subscribe_decode_failed", "err", err)
		h.respondError(r.Context(), w, http.StatusBadRequest, "invalid request body")
		return
	}

	var extra interface{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected trailing data")
		}
		h.log.Error(r.Context(), "subscribe_decode_failed", "err", err)
		h.respondError(r.Context(), w, http.StatusBadRequest, "invalid request body")
		return
	}

	if h.log.Enabled(r.Context(), logger.LevelDebug) {
		h.log.Debug(r.Context(), "subscribe_request", "body", bodyForLog(map[string]any{
			"email": req.Email,
			"repo":  req.Repo,
		}))
	}

	if err := h.svc.Subscribe(r.Context(), req.Email, req.Repo); err != nil {
		h.writeServiceError(r.Context(), w, err)
		return
	}

	h.respondJSON(r.Context(), w, http.StatusOK, map[string]string{
		"message": "Subscription created. Please confirm via email.",
	})
}
