package middleware_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github-release-notifier/internal/api/rest/middleware"
	"github-release-notifier/internal/platform/logger"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newBufferedLogger() (*logger.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return logger.NewWithWriter(logger.Config{Level: "info"}, buf), buf
}

func decodeLogEntries(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		entries = append(entries, entry)
	}
	return entries
}

func TestAccessLog_LogsRequestMetadata(t *testing.T) {
	log, buf := newBufferedLogger()
	h := middleware.AccessLog(log)(accessOKHandler())

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	req.RemoteAddr = "203.0.113.5:55555"
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	entries := decodeLogEntries(t, buf)
	require.Len(t, entries, 1)
	entry := entries[0]
	assert.Equal(t, "GET", entry["method"])
	assert.Equal(t, "/health", entry["path"])
	assert.Equal(t, "203.0.113.5:55555", entry["remote"])
	assert.EqualValues(t, http.StatusOK, entry["status"])
}

func TestAccessLog_RedactsTokenFromConfirmPath(t *testing.T) {
	log, buf := newBufferedLogger()

	r := chi.NewRouter()
	r.Use(middleware.AccessLog(log))
	r.Get("/api/confirm/{token}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/confirm/super-secret-bearer-token")
	require.NoError(t, err)
	defer resp.Body.Close()

	entries := decodeLogEntries(t, buf)
	require.Len(t, entries, 1)
	logged := entries[0]["path"].(string)
	assert.NotContains(t, logged, "super-secret-bearer-token",
		"raw confirm token must not appear in logs")
	assert.Contains(t, logged, "/api/confirm/{token}")
}

func TestAccessLog_RedactsEmailQueryParam(t *testing.T) {
	log, buf := newBufferedLogger()

	r := chi.NewRouter()
	r.Use(middleware.AccessLog(log))
	r.Get("/api/subscriptions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/subscriptions?email=alice%40example.com")
	require.NoError(t, err)
	defer resp.Body.Close()

	entries := decodeLogEntries(t, buf)
	require.Len(t, entries, 1)
	logged := entries[0]["path"].(string)
	assert.NotContains(t, strings.ToLower(logged), "alice@example.com",
		"raw email PII must not appear in logs")
	assert.True(t,
		strings.Contains(logged, "<redacted>") || strings.Contains(logged, "%3Credacted%3E"),
		"redacted placeholder missing from log entry: %s", logged)
}

func accessOKHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}
