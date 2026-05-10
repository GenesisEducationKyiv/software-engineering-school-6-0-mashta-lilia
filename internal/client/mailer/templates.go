package mailer

import (
	"fmt"
	"github-release-notifier/internal/model"
)

// Message is a fully composed email — subject, body, and recipient — ready
// to hand to a transport. Keeping it as a plain value object lets us test
// templating without ever touching SMTP, and lets the SMTP transport ignore
// where the content came from.
type Message struct {
	To      string
	Subject string
	Body    string
}

// TemplateBuilder turns domain inputs (an email + a token + a repo, or a
// release event) into a Message. This responsibility used to live inside
// SMTPMailer, which mixed templating with transport — a SRP violation and
// inappropriate coupling between mailer and BaseURL routing.
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

func (t *TemplateBuilder) ReleaseNotification(email, repo string, release *model.Release) Message {
	return Message{
		To:      email,
		Subject: fmt.Sprintf("New release for %s: %s", repo, release.TagName),
		Body: fmt.Sprintf(
			"A new release has been published for %s!\n\n"+
				"Version: %s\n"+
				"Name: %s\n"+
				"URL: %s\n",
			repo, release.TagName, release.Name, release.HTMLURL,
		),
	}
}
