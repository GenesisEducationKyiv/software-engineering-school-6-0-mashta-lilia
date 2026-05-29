package api_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github-release-notifier/internal/subscription"
	"github-release-notifier/tests/pkg/testdb"

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
	app := envForTest(t)
	testdb.TruncateAll(t, app.DB)

	resp := doGet(t, app.Server.URL+"/api/subscriptions?email=alice@example.com", nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestIntegration_List_RejectsWrongAPIKey(t *testing.T) {
	app := envForTest(t)
	testdb.TruncateAll(t, app.DB)

	resp := doGet(
		t,
		app.Server.URL+"/api/subscriptions?email=alice@example.com",
		map[string]string{"X-API-Key": "wrong-key"},
	)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestIntegration_List_HappyPath_ReturnsActiveOnly(t *testing.T) {
	app := envForTest(t)
	testdb.TruncateAll(t, app.DB)

	testdb.SeedSubscription(t, app.DB, "alice@example.com", "golang", "go", "tok-list-active",
		subscription.StatusActive)
	testdb.SeedSubscription(t, app.DB, "alice@example.com", "rust-lang", "rust", "tok-list-pending",
		subscription.StatusPending)
	testdb.SeedSubscription(t, app.DB, "bob@example.com", "golang", "go", "tok-list-other",
		subscription.StatusActive)

	q := url.Values{}
	q.Set("email", "alice@example.com")

	resp := doGet(
		t,
		app.Server.URL+"/api/subscriptions?"+q.Encode(),
		map[string]string{"X-API-Key": app.APIKey},
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
	app := envForTest(t)
	testdb.TruncateAll(t, app.DB)

	resp := doGet(
		t,
		app.Server.URL+"/api/subscriptions",
		map[string]string{"X-API-Key": app.APIKey},
	)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestIntegration_List_InvalidEmail(t *testing.T) {
	app := envForTest(t)
	testdb.TruncateAll(t, app.DB)

	resp := doGet(
		t,
		app.Server.URL+"/api/subscriptions?email=not-an-email",
		map[string]string{"X-API-Key": app.APIKey},
	)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
