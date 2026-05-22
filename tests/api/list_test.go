package api_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github-release-notifier/internal/subscription"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func doGet(t *testing.T, fullURL string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, fullURL, http.NoBody)
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func TestIntegration_List_RejectsMissingAPIKey(t *testing.T) {
	env := envForTest(t)

	resp := doGet(t, env.server.URL+"/api/subscriptions?email=alice@example.com", nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestIntegration_List_RejectsWrongAPIKey(t *testing.T) {
	env := envForTest(t)

	resp := doGet(
		t,
		env.server.URL+"/api/subscriptions?email=alice@example.com",
		map[string]string{"X-API-Key": "wrong-key"},
	)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestIntegration_List_HappyPath_ReturnsActiveOnly(t *testing.T) {
	env := envForTest(t)
	env.resetDB(t)

	env.seedSubscription(t, "alice@example.com", "golang", "go", "tok-list-active",
		subscription.StatusActive)
	env.seedSubscription(t, "alice@example.com", "rust-lang", "rust", "tok-list-pending",
		subscription.StatusPending)
	env.seedSubscription(t, "bob@example.com", "golang", "go", "tok-list-other",
		subscription.StatusActive)

	q := url.Values{}
	q.Set("email", "alice@example.com")

	resp := doGet(
		t,
		env.server.URL+"/api/subscriptions?"+q.Encode(),
		map[string]string{"X-API-Key": env.apiKey},
	)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var subs []subscription.Subscription
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&subs))
	require.Len(t, subs, 1, "only Alice's active sub should be returned")
	assert.Equal(t, "alice@example.com", subs[0].Email)
	assert.Equal(t, "golang", subs[0].RepoOwner)
	assert.Equal(t, subscription.StatusActive, subs[0].Status)
}

func TestIntegration_List_RequiresEmailQueryParam(t *testing.T) {
	env := envForTest(t)

	resp := doGet(
		t,
		env.server.URL+"/api/subscriptions",
		map[string]string{"X-API-Key": env.apiKey},
	)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestIntegration_List_InvalidEmail(t *testing.T) {
	env := envForTest(t)

	resp := doGet(
		t,
		env.server.URL+"/api/subscriptions?email=not-an-email",
		map[string]string{"X-API-Key": env.apiKey},
	)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
