package mailer_test

import (
	"strings"
	"testing"

	"github-release-notifier/internal/client/mailer"
	"github-release-notifier/internal/release"

	"github.com/stretchr/testify/assert"
)

func TestTemplateBuilder_Confirmation_IncludesURLAndRepo(t *testing.T) {
	tb := mailer.NewTemplateBuilder("https://example.com")
	msg := tb.Confirmation("alice@example.com", "tok123", "golang/go")

	assert.Equal(t, "alice@example.com", msg.To)
	assert.Contains(t, msg.Subject, "golang/go")
	assert.Contains(t, msg.Subject, "Confirm")
	assert.Contains(t, msg.Body, "https://example.com/api/confirm/tok123",
		"body must contain the full confirmation URL")
	assert.Contains(t, msg.Body, "golang/go")
}

func TestTemplateBuilder_Confirmation_StripsCRLFInjection(t *testing.T) {
	tb := mailer.NewTemplateBuilder("https://example.com")
	// Header injection attempt — newlines must be stripped from `To`
	// and from subject inputs so attackers cannot inject extra MIME
	// headers like Bcc: through the email argument.
	msg := tb.Confirmation("alice@example.com\r\nBcc: evil@bad.com", "tok", "golang/go\r\nX-Evil: 1")

	assert.NotContains(t, msg.To, "\n")
	assert.NotContains(t, msg.To, "\r")
	assert.NotContains(t, msg.Subject, "\n")
	assert.NotContains(t, msg.Subject, "\r")
}

func TestTemplateBuilder_ReleaseNotification_Standard(t *testing.T) {
	tb := mailer.NewTemplateBuilder("https://example.com")
	rel := &release.Release{
		TagName: "v1.22.0",
		Name:    "Go 1.22",
		HTMLURL: "https://github.com/golang/go/releases/tag/v1.22.0",
	}
	msg := tb.ReleaseNotification("alice@example.com", "golang/go", rel)

	assert.Equal(t, "alice@example.com", msg.To)
	assert.Contains(t, msg.Subject, "golang/go")
	assert.Contains(t, msg.Subject, "v1.22.0")
	assert.Contains(t, msg.Body, "v1.22.0")
	assert.Contains(t, msg.Body, "Go 1.22")
	assert.Contains(t, msg.Body, rel.HTMLURL)
}

func TestTemplateBuilder_ReleaseNotification_NilRelease_DoesNotPanic(t *testing.T) {
	tb := mailer.NewTemplateBuilder("https://example.com")

	assert.NotPanics(t, func() {
		msg := tb.ReleaseNotification("alice@example.com", "golang/go", nil)
		assert.Equal(t, "alice@example.com", msg.To)
		assert.Contains(t, strings.ToLower(msg.Subject), "new release")
		assert.NotEmpty(t, msg.Body)
	})
}
