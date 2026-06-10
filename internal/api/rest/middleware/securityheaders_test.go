package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github-release-notifier/internal/api/rest/middleware"

	"github.com/stretchr/testify/assert"
)

func TestSecurityHeaders_AlwaysSetOnAllPaths(t *testing.T) {
	t.Parallel()
	h := middleware.SecurityHeaders(okHandler())

	for _, path := range []string{"/", "/health", "/api/subscribe", "/metrics"} {
		req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), "path=%s", path)
		assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"), "path=%s", path)
		assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"), "path=%s", path)
	}
}

func TestSecurityHeaders_CacheControlNoStore_OnlyForAPI(t *testing.T) {
	t.Parallel()
	h := middleware.SecurityHeaders(okHandler())

	cases := []struct {
		path        string
		wantNoStore bool
	}{
		{"/api/subscribe", true},
		{"/api/confirm/abc", true},
		{"/api/unsubscribe/abc", true},
		{"/api/subscriptions", true},
		{"/", false},
		{"/health", false},
		{"/metrics", false},
		{"/swagger.yaml", false},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, http.NoBody)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		got := rec.Header().Get("Cache-Control")
		if tc.wantNoStore {
			assert.Equal(t, "no-store", got, "path=%s should be no-store", tc.path)
		} else {
			assert.Empty(t, got, "path=%s should not have Cache-Control set", tc.path)
		}
	}
}
