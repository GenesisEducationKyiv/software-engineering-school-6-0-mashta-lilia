package api_test

import (
	"net/http"
	"testing"

	"github-release-notifier/internal/subscription"
	"github-release-notifier/tests/pkg/testdb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_Confirm_FlipsPendingToActive(t *testing.T) {
	app := envForTest(t)
	testdb.TruncateAll(t, app.DB)

	testdb.SeedSubscription(t, app.DB, "alice@example.com", "golang", "go", "tok-pending",
		subscription.StatusPending)

	resp, err := http.Get(app.Server.URL + "/api/confirm/tok-pending")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, subscription.StatusActive, testdb.StatusOf(t, app.DB, "tok-pending"))
}

func TestIntegration_Confirm_UnknownToken_Returns404(t *testing.T) {
	app := envForTest(t)
	testdb.TruncateAll(t, app.DB)

	resp, err := http.Get(app.Server.URL + "/api/confirm/nope-not-a-real-token")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestIntegration_Confirm_AlreadyActive_IsIdempotent(t *testing.T) {
	app := envForTest(t)
	testdb.TruncateAll(t, app.DB)

	testdb.SeedSubscription(t, app.DB, "alice@example.com", "golang", "go", "tok-active",
		subscription.StatusActive)

	resp, err := http.Get(app.Server.URL + "/api/confirm/tok-active")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	// Already-active confirmation returns 200 (service treats it as idempotent).
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, subscription.StatusActive, testdb.StatusOf(t, app.DB, "tok-active"))
}
