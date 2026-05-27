package api_test

import (
	"net/http"
	"testing"

	"github-release-notifier/internal/subscription"
	"github-release-notifier/tests/pkg/testapp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_Unsubscribe_FlipsActiveToUnsubscribed(t *testing.T) {
	app := envForTest(t)
	testapp.TruncateAll(t, app.DB)

	testapp.SeedSubscription(t, app.DB, "alice@example.com", "golang", "go", "tok-unsub-1",
		subscription.StatusActive)

	resp, err := http.Get(app.Server.URL + "/api/unsubscribe/tok-unsub-1")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, subscription.StatusUnsubscribed, testapp.StatusOf(t, app.DB, "tok-unsub-1"))
}

func TestIntegration_Unsubscribe_UnknownToken_Returns404(t *testing.T) {
	app := envForTest(t)
	testapp.TruncateAll(t, app.DB)

	resp, err := http.Get(app.Server.URL + "/api/unsubscribe/does-not-exist")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestIntegration_Unsubscribe_AlreadyUnsubscribed_IsIdempotent(t *testing.T) {
	app := envForTest(t)
	testapp.TruncateAll(t, app.DB)

	testapp.SeedSubscription(t, app.DB, "alice@example.com", "golang", "go", "tok-unsub-2",
		subscription.StatusUnsubscribed)

	resp, err := http.Get(app.Server.URL + "/api/unsubscribe/tok-unsub-2")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, subscription.StatusUnsubscribed, testapp.StatusOf(t, app.DB, "tok-unsub-2"))
}
