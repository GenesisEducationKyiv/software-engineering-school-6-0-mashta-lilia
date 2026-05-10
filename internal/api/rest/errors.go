package rest

import (
	"errors"
	"github-release-notifier/internal/service"
	"net/http"
)

// errorMapping declares the HTTP response that corresponds to a given
// service-layer sentinel error. Adding a new business error means adding
// one entry here — handlers do not need to change.
type errorMapping struct {
	sentinel error
	status   int
	message  string
}

var serviceErrorMappings = []errorMapping{
	{service.ErrInvalidRepo, http.StatusBadRequest, "invalid repository format, expected owner/repo"},
	{service.ErrInvalidEmail, http.StatusBadRequest, "invalid email address"},
	{service.ErrRepoNotFound, http.StatusNotFound, "repository not found on GitHub"},
	{service.ErrAlreadyExists, http.StatusConflict, "subscription already exists"},
	{
		service.ErrEmailSendFailed, http.StatusServiceUnavailable,
		"failed to send confirmation email, please try again",
	},
	{service.ErrTokenNotFound, http.StatusNotFound, "invalid or expired token"},
	{service.ErrSubscriptionInactive, http.StatusNotFound, "invalid or expired token"},
}

// writeServiceError translates a service-layer error into an HTTP response.
// Unknown errors fall through to 500 Internal Server Error.
func writeServiceError(w http.ResponseWriter, err error) {
	for _, m := range serviceErrorMappings {
		if errors.Is(err, m.sentinel) {
			respondError(w, m.status, m.message)
			return
		}
	}
	respondError(w, http.StatusInternalServerError, "internal server error")
}
