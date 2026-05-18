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
	// Deliberate collapse: both sentinels return identical 404 + message so
	// the HTTP layer does not leak whether a token ever existed vs. exists
	// but was unsubscribed. The domain keeps them distinct for tests/logs.
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
