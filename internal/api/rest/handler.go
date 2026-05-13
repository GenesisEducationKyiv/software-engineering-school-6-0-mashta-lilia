package rest

import (
	"encoding/json"
	"github-release-notifier/internal/service"
	"log/slog"
	"net/http"
)

type Handler struct {
	svc *service.SubscriptionService
}

func NewHandler(svc *service.SubscriptionService) *Handler {
	return &Handler{svc: svc}
}

type errorResponse struct {
	Error string `json:"error"`
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, errorResponse{Error: msg})
}
