package resthttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/services/notification"
	"github-release-notifier/services/notification/resthttp"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockSender struct{ mock.Mock }

func (m *mockSender) SendConfirmation(ctx context.Context, c notification.Confirmation) (bool, error) {
	args := m.Called(ctx, c)
	return args.Bool(0), args.Error(1)
}

func newHandler(t *testing.T, sender *mockSender) *resthttp.Handler {
	t.Helper()
	h, err := resthttp.NewHandler(sender, logger.Nop())
	require.NoError(t, err)
	return h
}

func doVerify(h *resthttp.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/verify-email", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)
	return rec
}

func TestVerifyEmail_Success(t *testing.T) {
	t.Parallel()
	sender := &mockSender{}
	sender.On("SendConfirmation", mock.Anything, mock.Anything).Return(true, nil)

	rec := doVerify(newHandler(t, sender),
		`{"email":"a@b.c","confirm_url":"https://x/confirm/tok","repo":"o/r"}`)

	assert.Equal(t, http.StatusOK, rec.Code)
	var out struct {
		Delivered bool `json:"delivered"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.True(t, out.Delivered)
}

func TestVerifyEmail_MissingFields(t *testing.T) {
	t.Parallel()
	sender := &mockSender{}
	rec := doVerify(newHandler(t, sender), `{"email":"","confirm_url":"","repo":""}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	sender.AssertNotCalled(t, "SendConfirmation", mock.Anything, mock.Anything)
}

func TestVerifyEmail_BadJSON(t *testing.T) {
	t.Parallel()
	sender := &mockSender{}
	rec := doVerify(newHandler(t, sender), `{bad json`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestVerifyEmail_ServiceError(t *testing.T) {
	t.Parallel()
	sender := &mockSender{}
	sender.On("SendConfirmation", mock.Anything, mock.Anything).Return(false, errors.New("smtp down"))

	rec := doVerify(newHandler(t, sender),
		`{"email":"a@b.c","confirm_url":"https://x/confirm/tok","repo":"o/r"}`)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
