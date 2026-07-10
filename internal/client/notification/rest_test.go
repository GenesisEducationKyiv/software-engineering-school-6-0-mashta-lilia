package notification_test

import (
	"context"
	notificationclient "github-release-notifier/internal/client/notification"
	"github-release-notifier/internal/platform/logger"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRESTClient_VerifyEmail_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/verify-email", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"delivered":true}`))
	}))
	defer srv.Close()

	c, err := notificationclient.NewRESTClient(srv.URL, logger.Nop())
	require.NoError(t, err)

	delivered, err := c.VerifyEmail(context.Background(), "a@b.c", "https://x/confirm/tok", "o/r")
	require.NoError(t, err)
	assert.True(t, delivered)
}

func TestRESTClient_VerifyEmail_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	c, err := notificationclient.NewRESTClient(srv.URL, logger.Nop())
	require.NoError(t, err)

	_, err = c.VerifyEmail(context.Background(), "a@b.c", "https://x/confirm/tok", "o/r")
	require.Error(t, err)
}
