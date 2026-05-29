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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type serviceMock struct {
	mock.Mock
}

func (m *serviceMock) Subscribe(ctx context.Context, email, repo string) error {
	return m.Called(ctx, email, repo).Error(0)
}

func (m *serviceMock) Confirm(ctx context.Context, token string) error {
	return m.Called(ctx, token).Error(0)
}

func (m *serviceMock) Unsubscribe(ctx context.Context, token string) error {
	return m.Called(ctx, token).Error(0)
}

func (m *serviceMock) GetSubscriptions(ctx context.Context, email string) ([]subscription.Subscription, error) {
	args := m.Called(ctx, email)
	subs, _ := args.Get(0).([]subscription.Subscription)
	return subs, args.Error(1)
}

func newRouterWithHandler(h *resthandler.Handler) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/subscribe", h.Subscribe)
	r.Get("/api/confirm/{token}", h.Confirm)
	r.Get("/api/unsubscribe/{token}", h.Unsubscribe)
	r.Get("/api/subscriptions", h.List)
	return r
}

func newServer(t *testing.T) (*serviceMock, *httptest.Server) {
	t.Helper()
	m := &serviceMock{}
	srv := httptest.NewServer(newRouterWithHandler(resthandler.NewHandler(m)))
	t.Cleanup(srv.Close)
	return m, srv
}

func decodeError(t *testing.T, body []byte) string {
	t.Helper()
	var e struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &e))
	return e.Error
}

func TestSubscribe_Success(t *testing.T) {
	t.Parallel()
	m, srv := newServer(t)
	m.On("Subscribe", mock.Anything, "a@b.com", "golang/go").Return(nil).Once()

	resp, err := http.Post(srv.URL+"/api/subscribe", "application/json",
		strings.NewReader(`{"email":"a@b.com","repo":"golang/go"}`))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	m.AssertExpectations(t)
}

func TestSubscribe_MalformedJSON(t *testing.T) {
	t.Parallel()
	m, srv := newServer(t)

	resp, err := http.Post(srv.URL+"/api/subscribe", "application/json",
		strings.NewReader(`{not valid json`))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	m.AssertNotCalled(t, "Subscribe", mock.Anything, mock.Anything, mock.Anything)
}

func TestSubscribe_UnknownFieldsRejected(t *testing.T) {
	t.Parallel()
	m, srv := newServer(t)

	resp, err := http.Post(srv.URL+"/api/subscribe", "application/json",
		strings.NewReader(`{"email":"a@b.com","repo":"x/y","extra":"nope"}`))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	m.AssertNotCalled(t, "Subscribe", mock.Anything, mock.Anything, mock.Anything)
}

func TestSubscribe_TrailingGarbageRejected(t *testing.T) {
	t.Parallel()
	m, srv := newServer(t)

	resp, err := http.Post(srv.URL+"/api/subscribe", "application/json",
		strings.NewReader(`{"email":"a@b.com","repo":"x/y"}{"another":"object"}`))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
		"trailing data after JSON object should be rejected")
	m.AssertNotCalled(t, "Subscribe", mock.Anything, mock.Anything, mock.Anything)
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
			m, srv := newServer(t)
			m.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Return(tc.serviceErr).Once()

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

func TestConfirm_Success(t *testing.T) {
	t.Parallel()
	m, srv := newServer(t)
	m.On("Confirm", mock.Anything, "the-token").Return(nil).Once()

	resp, err := http.Get(srv.URL + "/api/confirm/the-token")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	m.AssertExpectations(t)
}

func TestConfirm_TokenNotFound_LeaksNoDifferenceFromInactive(t *testing.T) {
	t.Parallel()
	for _, e := range []error{subscription.ErrTokenNotFound, subscription.ErrSubscriptionInactive} {
		m, srv := newServer(t)
		m.On("Confirm", mock.Anything, mock.Anything).Return(e).Once()

		resp, err := http.Get(srv.URL + "/api/confirm/anything")
		require.NoError(t, err)
		body, readErr := readAll(t, resp)
		require.NoError(t, readErr)
		resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "err=%v", e)
		assert.Equal(t, "invalid or expired token", decodeError(t, body), "err=%v", e)
	}
}

func TestUnsubscribe_Success(t *testing.T) {
	t.Parallel()
	m, srv := newServer(t)
	m.On("Unsubscribe", mock.Anything, "the-token").Return(nil).Once()

	resp, err := http.Get(srv.URL + "/api/unsubscribe/the-token")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	m.AssertExpectations(t)
}

func TestUnsubscribe_TokenNotFound(t *testing.T) {
	t.Parallel()
	m, srv := newServer(t)
	m.On("Unsubscribe", mock.Anything, mock.Anything).Return(subscription.ErrTokenNotFound).Once()

	resp, err := http.Get(srv.URL + "/api/unsubscribe/anything")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestList_RequiresEmailQuery(t *testing.T) {
	t.Parallel()
	m, srv := newServer(t)

	resp, err := http.Get(srv.URL + "/api/subscriptions")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	m.AssertNotCalled(t, "GetSubscriptions", mock.Anything, mock.Anything)
}

func TestList_Success(t *testing.T) {
	t.Parallel()
	want := []subscription.Subscription{
		{ID: 1, Email: "a@b.com", RepoOwner: "golang", RepoName: "go", Status: subscription.StatusActive},
	}
	m, srv := newServer(t)
	m.On("GetSubscriptions", mock.Anything, "a@b.com").Return(want, nil).Once()

	resp, err := http.Get(srv.URL + "/api/subscriptions?email=a@b.com")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := readAll(t, resp)
	require.NoError(t, err)
	var got []subscription.Subscription
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, want, got)
	m.AssertExpectations(t)
}

func TestList_InvalidEmailFromService(t *testing.T) {
	t.Parallel()
	m, srv := newServer(t)
	// Registered so AssertExpectations would catch a regression where the handler short-circuits.
	m.On("GetSubscriptions", mock.Anything, "not-an-email").
		Return(nil, subscription.ErrInvalidEmail).Once()

	resp, err := http.Get(srv.URL + "/api/subscriptions?email=not-an-email")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	m.AssertExpectations(t)
}

func readAll(t *testing.T, resp *http.Response) ([]byte, error) {
	t.Helper()
	return io.ReadAll(resp.Body)
}
