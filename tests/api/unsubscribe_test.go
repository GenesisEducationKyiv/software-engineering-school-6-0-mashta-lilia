package api_test

import (
	"net/http"
	"testing"

	"github-release-notifier/internal/subscription"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_Unsubscribe_FlipsActiveToUnsubscribed(t *testing.T) {
	env := envForTest(t)
	env.resetDB(t)

	env.seedSubscription(t, "alice@example.com", "golang", "go", "tok-unsub-1",
		subscription.StatusActive)

	resp, err := http.Get(env.server.URL + "/api/unsubscribe/tok-unsub-1")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, subscription.StatusUnsubscribed, env.statusOf(t, "tok-unsub-1"))
}

func TestIntegration_Unsubscribe_UnknownToken_Returns404(t *testing.T) {
	env := envForTest(t)
	env.resetDB(t)

	resp, err := http.Get(env.server.URL + "/api/unsubscribe/does-not-exist")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestIntegration_Unsubscribe_AlreadyUnsubscribed_IsIdempotent(t *testing.T) {
	env := envForTest(t)
	env.resetDB(t)

	env.seedSubscription(t, "alice@example.com", "golang", "go", "tok-unsub-2",
		subscription.StatusUnsubscribed)

	resp, err := http.Get(env.server.URL + "/api/unsubscribe/tok-unsub-2")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, subscription.StatusUnsubscribed, env.statusOf(t, "tok-unsub-2"))
}
