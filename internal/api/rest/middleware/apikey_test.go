package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github-release-notifier/internal/api/rest/middleware"
	"github-release-notifier/internal/platform/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestAPIKeyAuth_EmptyKey_FailsClosed(t *testing.T) {
	t.Parallel()
	h := middleware.APIKeyAuth("", logger.Nop())(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions", http.NoBody)
	req.Header.Set("X-API-Key", "anything")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"empty configured key must fail closed, never bypass")
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, strings.ToLower(body["error"]), "not configured")
}

func TestAPIKeyAuth_MissingHeader(t *testing.T) {
	t.Parallel()
	h := middleware.APIKeyAuth("expected-key", logger.Nop())(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions", http.NoBody)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body["error"], "invalid or missing API key")
}

func TestAPIKeyAuth_WrongKey(t *testing.T) {
	t.Parallel()
	h := middleware.APIKeyAuth("expected-key", logger.Nop())(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions", http.NoBody)
	req.Header.Set("X-API-Key", "wrong-key")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAPIKeyAuth_CorrectKey(t *testing.T) {
	t.Parallel()
	h := middleware.APIKeyAuth("expected-key", logger.Nop())(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions", http.NoBody)
	req.Header.Set("X-API-Key", "expected-key")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestAPIKeyAuth_RejectsKeysOfDifferingLength(t *testing.T) {
	t.Parallel()
	h := middleware.APIKeyAuth("expected-key-1234567890", logger.Nop())(okHandler())

	for _, attempt := range []string{"x", "x-very-very-very-long-and-still-wrong"} {
		req := httptest.NewRequest(http.MethodGet, "/api/subscriptions", http.NoBody)
		req.Header.Set("X-API-Key", attempt)
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code, "attempt=%q", attempt)
	}
}
