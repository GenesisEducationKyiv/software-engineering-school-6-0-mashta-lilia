package smtp

import (
	"fmt"
	"github-release-notifier/services/notification"
)

type Message struct {
	To      string
	Subject string
	Body    string
}

type TemplateBuilder struct{}

func NewTemplateBuilder() *TemplateBuilder {
	return &TemplateBuilder{}
}

// confirmURL is built by the monolith, which owns the public base URL and route; the
// notifier only renders the link it is handed and never constructs subscription URLs.
func (t *TemplateBuilder) Confirmation(email, confirmURL, repo string) Message {
	safeEmail := sanitizeHeader(email)
	safeRepo := sanitizeHeader(repo)
	return Message{
		To:      safeEmail,
		Subject: fmt.Sprintf("Confirm your subscription to %s releases", safeRepo),
		Body: fmt.Sprintf(
			"You have subscribed to release notifications for %s.\n\n"+
				"Please confirm your subscription by clicking the link below:\n%s\n\n"+
				"If you did not request this, you can ignore this email.",
			safeRepo, confirmURL,
		),
	}
}

func (t *TemplateBuilder) ReleaseNotification(email, repo string, rel *notification.ReleaseInfo) Message {
	safeEmail := sanitizeHeader(email)
	safeRepo := sanitizeHeader(repo)
	if rel == nil {
		return Message{
			To:      safeEmail,
			Subject: fmt.Sprintf("New release for %s", safeRepo),
			Body:    fmt.Sprintf("A new release has been published for %s.\n", safeRepo),
		}
	}
	safeTag := sanitizeHeader(rel.TagName)
	return Message{
		To:      safeEmail,
		Subject: fmt.Sprintf("New release for %s: %s", safeRepo, safeTag),
		Body: fmt.Sprintf(
			"A new release has been published for %s!\n\n"+
				"Version: %s\n"+
				"Name: %s\n"+
				"URL: %s\n",
			safeRepo, rel.TagName, rel.Name, rel.HTMLURL,
		),
	}
}
