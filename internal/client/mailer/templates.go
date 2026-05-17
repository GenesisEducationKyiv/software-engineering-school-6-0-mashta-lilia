package mailer

import (
	"fmt"
	"github-release-notifier/internal/release"
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
	confirmURL := fmt.Sprintf("%s/api/confirm/%s", t.baseURL, token)
	return Message{
		To:      email,
		Subject: fmt.Sprintf("Confirm your subscription to %s releases", repo),
		Body: fmt.Sprintf(
			"You have subscribed to release notifications for %s.\n\n"+
				"Please confirm your subscription by clicking the link below:\n%s\n\n"+
				"If you did not request this, you can ignore this email.",
			repo, confirmURL,
		),
	}
}

func (t *TemplateBuilder) ReleaseNotification(email, repo string, rel *release.Release) Message {
	// rel may be nil — degrade gracefully rather than panic.
	if rel == nil {
		return Message{
			To:      email,
			Subject: fmt.Sprintf("New release for %s", repo),
			Body:    fmt.Sprintf("A new release has been published for %s.\n", repo),
		}
	}
	return Message{
		To:      email,
		Subject: fmt.Sprintf("New release for %s: %s", repo, rel.TagName),
		Body: fmt.Sprintf(
			"A new release has been published for %s!\n\n"+
				"Version: %s\n"+
				"Name: %s\n"+
				"URL: %s\n",
			repo, rel.TagName, rel.Name, rel.HTMLURL,
		),
	}
}
