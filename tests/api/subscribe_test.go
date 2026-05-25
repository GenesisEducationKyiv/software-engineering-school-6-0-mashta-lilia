package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github-release-notifier/internal/subscription"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_Subscribe_HappyPath(t *testing.T) {
	env := envForTest(t)
	env.resetDB(t)
	env.github.reset()
	env.github.SetRepoExists("golang", "go", true)
	require.NoError(t, env.mailpit.reset(context.Background()))

	resp, err := http.Post(
		env.server.URL+"/api/subscribe",
		"application/json",
		strings.NewReader(`{"email":"alice@example.com","repo":"golang/go"}`),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Mailpit must have received exactly one confirmation email containing
	// the confirm URL (which embeds the token from the DB).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	msg, err := env.mailpit.waitForMessage(ctx, 10*time.Second)
	require.NoError(t, err)
	require.Len(t, msg.To, 1)
	assert.Equal(t, "alice@example.com", msg.To[0].Address)
	assert.Contains(t, msg.Subject, "golang/go")

	body, err := env.mailpit.messageBody(ctx, msg.ID)
	require.NoError(t, err)
	assert.Contains(t, body, "http://test.local/api/confirm/",
		"confirmation URL with token must be present in the email body")
}

// NOTE: per the Practical Test Pyramid (see testing.md), validation edge
// cases — malformed JSON, invalid email format, unknown fields — live as
// unit tests in internal/api/rest/subscription/handler_test.go. The
// integration layer only covers wiring + side-effects, not branching rules.

func TestIntegration_Subscribe_RepoNotFoundOnGitHub(t *testing.T) {
	env := envForTest(t)
	env.resetDB(t)
	env.github.reset()
	require.NoError(t, env.mailpit.reset(context.Background()))
	// fake github reports the repo does not exist
	env.github.SetRepoExists("ghost", "repo", false)

	resp, err := http.Post(
		env.server.URL+"/api/subscribe",
		"application/json",
		strings.NewReader(`{"email":"alice@example.com","repo":"ghost/repo"}`),
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestIntegration_Subscribe_DuplicateActiveSubscription(t *testing.T) {
	env := envForTest(t)
	env.resetDB(t)
	env.github.reset()
	require.NoError(t, env.mailpit.reset(context.Background()))
	env.github.SetRepoExists("golang", "go", true)

	env.seedSubscription(t, "alice@example.com", "golang", "go", "tok-pre", subscription.StatusActive)

	resp, err := http.Post(
		env.server.URL+"/api/subscribe",
		"application/json",
		strings.NewReader(`{"email":"alice@example.com","repo":"golang/go"}`),
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}
