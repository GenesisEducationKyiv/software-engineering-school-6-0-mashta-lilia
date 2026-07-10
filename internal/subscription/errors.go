package subscription

import "errors"

var (
	ErrInvalidRepo          = errors.New("invalid repository format, expected owner/repo")
	ErrRepoNotFound         = errors.New("repository not found on GitHub")
	ErrAlreadyExists        = errors.New("subscription already exists")
	ErrTokenNotFound        = errors.New("subscription token not found")
	ErrSubscriptionInactive = errors.New("subscription is not active")
	ErrInvalidEmail         = errors.New("invalid email address")
	ErrEmailSendFailed      = errors.New("failed to send email")
	ErrNotFound             = errors.New("subscription not found")
)
