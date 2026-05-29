package subscription

import (
	"errors"
	"github-release-notifier/internal/subscription"
	"net/http"
)

type errorMapping struct {
	sentinel error
	status   int
	message  string
}

var serviceErrorMappings = []errorMapping{
	{subscription.ErrInvalidRepo, http.StatusBadRequest, "invalid repository format, expected owner/repo"},
	{subscription.ErrInvalidEmail, http.StatusBadRequest, "invalid email address"},
	{subscription.ErrRepoNotFound, http.StatusNotFound, "repository not found on GitHub"},
	{subscription.ErrAlreadyExists, http.StatusConflict, "subscription already exists"},
	{
		subscription.ErrEmailSendFailed, http.StatusServiceUnavailable,
		"failed to send confirmation email, please try again",
	},
	// Both sentinels collapse to the same 404 so HTTP doesn't leak whether a token ever existed.
	{subscription.ErrTokenNotFound, http.StatusNotFound, "invalid or expired token"},
	{subscription.ErrSubscriptionInactive, http.StatusNotFound, "invalid or expired token"},
}

func writeServiceError(w http.ResponseWriter, err error) {
	for _, m := range serviceErrorMappings {
		if errors.Is(err, m.sentinel) {
			respondError(w, m.status, m.message)
			return
		}
	}
	respondError(w, http.StatusInternalServerError, "internal server error")
}
