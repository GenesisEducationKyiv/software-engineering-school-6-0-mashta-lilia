package subscription_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	resthandler "github-release-notifier/internal/api/rest/subscription"
	"github-release-notifier/internal/subscription"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeService implements the unexported subscriptionService interface
// structurally — Go's interface satisfaction is duck-typed, so we don't
// need to reference the interface name from outside the package.
type fakeService struct {
	subscribeErr   error
	confirmErr     error
	unsubscribeErr error
	listResult     []subscription.Subscription
	listErr        error

	gotEmail string
	gotRepo  string
	gotToken string
	callCnt  int
}

func (f *fakeService) Subscribe(_ context.Context, email, repo string) error {
	f.callCnt++
	f.gotEmail = email
	f.gotRepo = repo
	return f.subscribeErr
}

func (f *fakeService) Confirm(_ context.Context, token string) error {
	f.callCnt++
	f.gotToken = token
	return f.confirmErr
}

func (f *fakeService) Unsubscribe(_ context.Context, token string) error {
	f.callCnt++
	f.gotToken = token
	return f.unsubscribeErr
}

func (f *fakeService) GetSubscriptions(_ context.Context, email string) ([]subscription.Subscription, error) {
	f.callCnt++
	f.gotEmail = email
	return f.listResult, f.listErr
}

func newRouterWithHandler(h *resthandler.Handler) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/subscribe", h.Subscribe)
	r.Get("/api/confirm/{token}", h.Confirm)
	r.Get("/api/unsubscribe/{token}", h.Unsubscribe)
	r.Get("/api/subscriptions", h.List)
	return r
}

func decodeError(t *testing.T, body []byte) string {
	t.Helper()
	var e struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &e))
	return e.Error
}

// --- Subscribe ---

func TestSubscribe_Success(t *testing.T) {
	t.Parallel()
	fs := &fakeService{}
	srv := httptest.NewServer(newRouterWithHandler(resthandler.NewHandler(fs)))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/subscribe", "application/json",
		strings.NewReader(`{"email":"a@b.com","repo":"golang/go"}`))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "a@b.com", fs.gotEmail)
	assert.Equal(t, "golang/go", fs.gotRepo)
}

func TestSubscribe_MalformedJSON(t *testing.T) {
	t.Parallel()
	fs := &fakeService{}
	srv := httptest.NewServer(newRouterWithHandler(resthandler.NewHandler(fs)))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/subscribe", "application/json",
		strings.NewReader(`{not valid json`))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Zero(t, fs.callCnt, "service must not be called on bad JSON")
}

func TestSubscribe_UnknownFieldsRejected(t *testing.T) {
	t.Parallel()
	fs := &fakeService{}
	srv := httptest.NewServer(newRouterWithHandler(resthandler.NewHandler(fs)))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/subscribe", "application/json",
		strings.NewReader(`{"email":"a@b.com","repo":"x/y","extra":"nope"}`))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Zero(t, fs.callCnt, "service must not be called on unknown fields")
}

func TestSubscribe_TrailingGarbageRejected(t *testing.T) {
	t.Parallel()
	fs := &fakeService{}
	srv := httptest.NewServer(newRouterWithHandler(resthandler.NewHandler(fs)))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/subscribe", "application/json",
		strings.NewReader(`{"email":"a@b.com","repo":"x/y"}{"another":"object"}`))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
		"trailing data after JSON object should be rejected")
	assert.Zero(t, fs.callCnt, "service must not be called on trailing garbage")
}

func TestSubscribe_ErrorMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		serviceErr error
		wantStatus int
		wantMsg    string
	}{
		{"invalid email", subscription.ErrInvalidEmail, http.StatusBadRequest, "invalid email address"},
		{"invalid repo", subscription.ErrInvalidRepo, http.StatusBadRequest, "invalid repository format, expected owner/repo"},
		{"repo not found", subscription.ErrRepoNotFound, http.StatusNotFound, "repository not found on GitHub"},
		{"already exists", subscription.ErrAlreadyExists, http.StatusConflict, "subscription already exists"},
		{"email send failed", subscription.ErrEmailSendFailed, http.StatusServiceUnavailable, "failed to send confirmation email, please try again"},
		{"unknown error", errors.New("boom"), http.StatusInternalServerError, "internal server error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := &fakeService{subscribeErr: tc.serviceErr}
			srv := httptest.NewServer(newRouterWithHandler(resthandler.NewHandler(fs)))
			defer srv.Close()

			resp, err := http.Post(srv.URL+"/api/subscribe", "application/json",
				strings.NewReader(`{"email":"a@b.com","repo":"x/y"}`))
			require.NoError(t, err)
			defer resp.Body.Close()

			body, err := readAll(t, resp)
			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, resp.StatusCode)
			assert.Equal(t, tc.wantMsg, decodeError(t, body))
		})
	}
}

// --- Confirm ---

func TestConfirm_Success(t *testing.T) {
	t.Parallel()
	fs := &fakeService{}
	srv := httptest.NewServer(newRouterWithHandler(resthandler.NewHandler(fs)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/confirm/the-token")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "the-token", fs.gotToken)
}

func TestConfirm_TokenNotFound_LeaksNoDifferenceFromInactive(t *testing.T) {
	t.Parallel()
	// Both ErrTokenNotFound and ErrSubscriptionInactive must produce
	// identical responses so a probing attacker can't distinguish
	// "never existed" from "already used".
	for _, e := range []error{subscription.ErrTokenNotFound, subscription.ErrSubscriptionInactive} {
		fs := &fakeService{confirmErr: e}
		srv := httptest.NewServer(newRouterWithHandler(resthandler.NewHandler(fs)))

		resp, err := http.Get(srv.URL + "/api/confirm/anything")
		require.NoError(t, err)
		body, readErr := readAll(t, resp)
		require.NoError(t, readErr)
		resp.Body.Close()
		srv.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "err=%v", e)
		assert.Equal(t, "invalid or expired token", decodeError(t, body), "err=%v", e)
	}
}

// --- Unsubscribe ---

func TestUnsubscribe_Success(t *testing.T) {
	t.Parallel()
	fs := &fakeService{}
	srv := httptest.NewServer(newRouterWithHandler(resthandler.NewHandler(fs)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/unsubscribe/the-token")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "the-token", fs.gotToken)
}

func TestUnsubscribe_TokenNotFound(t *testing.T) {
	t.Parallel()
	fs := &fakeService{unsubscribeErr: subscription.ErrTokenNotFound}
	srv := httptest.NewServer(newRouterWithHandler(resthandler.NewHandler(fs)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/unsubscribe/anything")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// --- List ---

func TestList_RequiresEmailQuery(t *testing.T) {
	t.Parallel()
	fs := &fakeService{}
	srv := httptest.NewServer(newRouterWithHandler(resthandler.NewHandler(fs)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/subscriptions")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Zero(t, fs.callCnt, "service must not be called when email is missing")
}

func TestList_Success(t *testing.T) {
	t.Parallel()
	fs := &fakeService{
		listResult: []subscription.Subscription{
			{ID: 1, Email: "a@b.com", RepoOwner: "golang", RepoName: "go", Status: subscription.StatusActive},
		},
	}
	srv := httptest.NewServer(newRouterWithHandler(resthandler.NewHandler(fs)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/subscriptions?email=a@b.com")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := readAll(t, resp)
	require.NoError(t, err)
	var got []subscription.Subscription
	require.NoError(t, json.Unmarshal(body, &got))
	require.Len(t, got, 1)
	assert.Equal(t, "a@b.com", got[0].Email)
	assert.Equal(t, "a@b.com", fs.gotEmail)
}

func TestList_InvalidEmailFromService(t *testing.T) {
	t.Parallel()
	fs := &fakeService{listErr: subscription.ErrInvalidEmail}
	srv := httptest.NewServer(newRouterWithHandler(resthandler.NewHandler(fs)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/subscriptions?email=not-an-email")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	// callCnt guard proves the service was actually reached. Without it the
	// test would still pass if the handler short-circuited on its own
	// validation — masking a regression where the service path stops being
	// exercised at all.
	assert.Equal(t, 1, fs.callCnt,
		"handler must forward to service; ErrInvalidEmail mapping is service-driven")
	assert.Equal(t, "not-an-email", fs.gotEmail)
}

// Helper to drain response body and preserve a copy for assertions.
// Body lifecycle is the caller's responsibility — pair with defer resp.Body.Close().
func readAll(t *testing.T, resp *http.Response) ([]byte, error) {
	t.Helper()
	return io.ReadAll(resp.Body)
}
