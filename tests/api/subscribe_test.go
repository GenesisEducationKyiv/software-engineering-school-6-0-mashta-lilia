package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github-release-notifier/internal/subscription"
	"github-release-notifier/tests/pkg/testdb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_Subscribe_HappyPath(t *testing.T) {
	app := envForTest(t)
	testdb.TruncateAll(t, app.DB)
	app.Github.Reset()
	app.Github.SetRepoExists("golang", "go", true)
	require.NoError(t, app.Mailpit.Reset(context.Background()))

	resp, err := http.Post(
		app.Server.URL+"/api/subscribe",
		"application/json",
		strings.NewReader(`{"email":"alice@example.com","repo":"golang/go"}`),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	msg, err := app.Mailpit.WaitForMessage(ctx, 10*time.Second)
	require.NoError(t, err)
	require.Len(t, msg.To, 1)
	assert.Equal(t, "alice@example.com", msg.To[0].Address)
	assert.Contains(t, msg.Subject, "golang/go")

	body, err := app.Mailpit.MessageBody(ctx, msg.ID)
	require.NoError(t, err)
	assert.Contains(t, body, "http://test.local/api/confirm/",
		"confirmation URL with token must be present in the email body")
}

func TestIntegration_Subscribe_RepoNotFoundOnGitHub(t *testing.T) {
	app := envForTest(t)
	testdb.TruncateAll(t, app.DB)
	app.Github.Reset()
	require.NoError(t, app.Mailpit.Reset(context.Background()))
	app.Github.SetRepoExists("ghost", "repo", false)

	resp, err := http.Post(
		app.Server.URL+"/api/subscribe",
		"application/json",
		strings.NewReader(`{"email":"alice@example.com","repo":"ghost/repo"}`),
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestIntegration_Subscribe_DuplicateActiveSubscription(t *testing.T) {
	app := envForTest(t)
	testdb.TruncateAll(t, app.DB)
	app.Github.Reset()
	require.NoError(t, app.Mailpit.Reset(context.Background()))
	app.Github.SetRepoExists("golang", "go", true)

	testdb.SeedSubscription(t, app.DB, "alice@example.com", "golang", "go", "tok-pre", subscription.StatusActive)

	resp, err := http.Post(
		app.Server.URL+"/api/subscribe",
		"application/json",
		strings.NewReader(`{"email":"alice@example.com","repo":"golang/go"}`),
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}
