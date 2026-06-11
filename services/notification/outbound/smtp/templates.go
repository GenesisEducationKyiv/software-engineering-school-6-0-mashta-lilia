package smtp

import (
	"fmt"
	"github-release-notifier/services/notification/model"
)

type Message struct {
	To      string
	Subject string
	Body    string
}

type TemplateBuilder struct {
	baseURL string
}

func NewTemplateBuilder(baseURL string) *TemplateBuilder {
	return &TemplateBuilder{baseURL: baseURL}
}

func (t *TemplateBuilder) Confirmation(email, token, repo string) Message {
	safeEmail := sanitizeHeader(email)
	safeRepo := sanitizeHeader(repo)
	confirmURL := fmt.Sprintf("%s/api/confirm/%s", t.baseURL, token)
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

func (t *TemplateBuilder) ReleaseNotification(email, repo string, rel *model.ReleaseInfo) Message {
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
