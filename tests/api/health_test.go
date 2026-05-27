package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github-release-notifier/tests/pkg/testapp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_Root_ReturnsServiceInfo(t *testing.T) {
	app := envForTest(t)
	testapp.TruncateAll(t, app.DB)

	resp, err := http.Get(app.Server.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "GitHub Release Notification API", body["service"])
	assert.Equal(t, "/health", body["health"])
}

func TestIntegration_Health_OK(t *testing.T) {
	app := envForTest(t)
	testapp.TruncateAll(t, app.DB)

	resp, err := http.Get(app.Server.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "healthy", body["status"])
}

func TestIntegration_Metrics_Exposed(t *testing.T) {
	app := envForTest(t)
	testapp.TruncateAll(t, app.DB)

	resp, err := http.Get(app.Server.URL + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	// Standard Prometheus expositions begin with a HELP comment.
	assert.Contains(t, string(body), "# HELP")
}

func TestIntegration_SecurityHeaders_PresentEverywhere(t *testing.T) {
	app := envForTest(t)
	testapp.TruncateAll(t, app.DB)

	resp, err := http.Get(app.Server.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", resp.Header.Get("X-Frame-Options"))
	assert.Equal(t, "no-referrer", resp.Header.Get("Referrer-Policy"))
}
