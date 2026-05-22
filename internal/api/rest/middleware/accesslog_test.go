package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github-release-notifier/internal/api/rest/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(orig) })
	return buf
}

func TestAccessLog_LogsRequestMetadata(t *testing.T) {
	buf := captureSlog(t)
	h := middleware.AccessLog(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	req.RemoteAddr = "203.0.113.5:55555"
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "GET", entry["method"])
	assert.Equal(t, "/health", entry["path"])
	assert.Equal(t, "203.0.113.5:55555", entry["remote"])
	assert.EqualValues(t, http.StatusOK, entry["status"])
}

// The AccessLog middleware must never log raw bearer tokens from URLs like
// /api/confirm/<token> — chi RoutePattern() is used to redact them.
func TestAccessLog_RedactsTokenFromConfirmPath(t *testing.T) {
	buf := captureSlog(t)

	r := chi.NewRouter()
	r.Use(middleware.AccessLog)
	r.Get("/api/confirm/{token}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/confirm/super-secret-bearer-token")
	require.NoError(t, err)
	defer resp.Body.Close()

	logged := buf.String()
	assert.NotContains(t, logged, "super-secret-bearer-token",
		"raw confirm token must not appear in logs")
	assert.Contains(t, logged, "/api/confirm/{token}")
}

func TestAccessLog_RedactsEmailQueryParam(t *testing.T) {
	buf := captureSlog(t)

	r := chi.NewRouter()
	r.Use(middleware.AccessLog)
	r.Get("/api/subscriptions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/subscriptions?email=alice%40example.com")
	require.NoError(t, err)
	defer resp.Body.Close()

	logged := buf.String()
	assert.NotContains(t, strings.ToLower(logged), "alice@example.com",
		"raw email PII must not appear in logs")
	// The redacted placeholder may appear URL-encoded (%3Credacted%3E)
	// or literal (<redacted>) depending on how the JSON encoder escapes
	// the slog string value — both prove the email was scrubbed.
	assert.True(t,
		strings.Contains(logged, "<redacted>") || strings.Contains(logged, "%3Credacted%3E"),
		"redacted placeholder missing from log entry: %s", logged)
}
