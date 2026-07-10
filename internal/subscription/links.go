package subscription

import (
	"fmt"
	"strings"
)

// ConfirmLinkBuilder builds the public confirmation URL for a pending subscription.
// The path mirrors the monolith REST route (GET /api/confirm/{token}); keeping it here
// stops the notification service from owning the monolith's URL scheme.
type ConfirmLinkBuilder struct {
	baseURL string
}

func NewConfirmLinkBuilder(baseURL string) ConfirmLinkBuilder {
	return ConfirmLinkBuilder{baseURL: strings.TrimRight(baseURL, "/")}
}

func (b ConfirmLinkBuilder) ConfirmURL(token string) string {
	return fmt.Sprintf("%s/api/confirm/%s", b.baseURL, token)
}
