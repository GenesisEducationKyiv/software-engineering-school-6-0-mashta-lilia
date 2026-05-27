package mailer_test

import (
	"testing"

	"github-release-notifier/internal/client/mailer"
	"github-release-notifier/internal/release"

	"github.com/stretchr/testify/assert"
)

func TestTemplateBuilder_Confirmation_Standard(t *testing.T) {
	t.Parallel()
	tb := mailer.NewTemplateBuilder("https://example.com")

	got := tb.Confirmation("alice@example.com", "tok123", "golang/go")

	want := mailer.Message{
		To:      "alice@example.com",
		Subject: "Confirm your subscription to golang/go releases",
		Body: "You have subscribed to release notifications for golang/go.\n\n" +
			"Please confirm your subscription by clicking the link below:\n" +
			"https://example.com/api/confirm/tok123\n\n" +
			"If you did not request this, you can ignore this email.",
	}
	assert.Equal(t, want, got)
}

func TestTemplateBuilder_Confirmation_StripsCRLFInjection(t *testing.T) {
	t.Parallel()
	tb := mailer.NewTemplateBuilder("https://example.com")
	// Header injection attempt — newlines must be stripped from To and from
	// subject inputs so attackers cannot inject extra MIME headers like Bcc:.
	got := tb.Confirmation(
		"alice@example.com\r\nBcc: evil@bad.com",
		"tok",
		"golang/go\r\nX-Evil: 1",
	)

	// Full-struct compare: the inputs that flow into headers must have CRLF
	// removed, AND the rest of the template must still render correctly —
	// sanitization should not nuke the body.
	want := mailer.Message{
		To:      "alice@example.comBcc: evil@bad.com",
		Subject: "Confirm your subscription to golang/goX-Evil: 1 releases",
		Body: "You have subscribed to release notifications for golang/goX-Evil: 1.\n\n" +
			"Please confirm your subscription by clicking the link below:\n" +
			"https://example.com/api/confirm/tok\n\n" +
			"If you did not request this, you can ignore this email.",
	}
	assert.Equal(t, want, got)
}

func TestTemplateBuilder_ReleaseNotification_Standard(t *testing.T) {
	t.Parallel()
	tb := mailer.NewTemplateBuilder("https://example.com")
	rel := &release.Release{
		TagName: "v1.22.0",
		Name:    "Go 1.22",
		HTMLURL: "https://github.com/golang/go/releases/tag/v1.22.0",
	}

	got := tb.ReleaseNotification("alice@example.com", "golang/go", rel)

	want := mailer.Message{
		To:      "alice@example.com",
		Subject: "New release for golang/go: v1.22.0",
		Body: "A new release has been published for golang/go!\n\n" +
			"Version: v1.22.0\n" +
			"Name: Go 1.22\n" +
			"URL: https://github.com/golang/go/releases/tag/v1.22.0\n",
	}
	assert.Equal(t, want, got)
}

func TestTemplateBuilder_ReleaseNotification_StripsCRLFInjection(t *testing.T) {
	t.Parallel()
	tb := mailer.NewTemplateBuilder("https://example.com")
	// Header injection attempt — newlines must be stripped from To, the repo
	// (subject input), and the tag (which also lands in the Subject).
	rel := &release.Release{
		TagName: "v1.0\r\nX-Evil: 1",
		Name:    "ok",
		HTMLURL: "https://example.com/r",
	}
	got := tb.ReleaseNotification(
		"alice@example.com\r\nBcc: evil@bad.com",
		"golang/go\r\nX-Evil: 1",
		rel,
	)

	want := mailer.Message{
		To:      "alice@example.comBcc: evil@bad.com",
		Subject: "New release for golang/goX-Evil: 1: v1.0X-Evil: 1",
		// The body intentionally renders the raw TagName: it's content, not
		// a header, and sanitizing it would distort what the user sees. The
		// SMTP envelope only puts sanitized values into header positions.
		Body: "A new release has been published for golang/goX-Evil: 1!\n\n" +
			"Version: v1.0\r\nX-Evil: 1\n" +
			"Name: ok\n" +
			"URL: https://example.com/r\n",
	}
	assert.Equal(t, want, got)
}

func TestTemplateBuilder_ReleaseNotification_NilRelease(t *testing.T) {
	t.Parallel()
	tb := mailer.NewTemplateBuilder("https://example.com")

	got := tb.ReleaseNotification("alice@example.com", "golang/go", nil)

	want := mailer.Message{
		To:      "alice@example.com",
		Subject: "New release for golang/go",
		Body:    "A new release has been published for golang/go.\n",
	}
	assert.Equal(t, want, got)
}
