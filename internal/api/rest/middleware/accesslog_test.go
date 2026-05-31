package middleware_test

import (
	"context"
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

type logEntry struct {
	ctx    context.Context
	msg    string
	fields map[string]any
}

type recordingLogger struct {
	entries []logEntry
}

func (l *recordingLogger) Debug(ctx context.Context, msg string, kv ...any) {
	l.record(ctx, msg, kv...)
}

func (l *recordingLogger) Info(ctx context.Context, msg string, kv ...any) {
	l.record(ctx, msg, kv...)
}

func (l *recordingLogger) Warn(ctx context.Context, msg string, kv ...any) {
	l.record(ctx, msg, kv...)
}

func (l *recordingLogger) Error(ctx context.Context, msg string, kv ...any) {
	l.record(ctx, msg, kv...)
}

func (l *recordingLogger) With(_ ...any) logger.Logger {
	return l
}

func (l *recordingLogger) Enabled(context.Context, logger.Level) bool { return true }

func (l *recordingLogger) record(ctx context.Context, msg string, kv ...any) {
	fields := make(map[string]any, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		key, _ := kv[i].(string)
		fields[key] = kv[i+1]
	}
	l.entries = append(l.entries, logEntry{ctx: ctx, msg: msg, fields: fields})
}

func TestAccessLog_LogsRequestMetadata(t *testing.T) {
	log := &recordingLogger{}
	h := middleware.AccessLog(log)(accessOKHandler())

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	req.RemoteAddr = "203.0.113.5:55555"
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	require.Len(t, log.entries, 1)
	entry := log.entries[0]
	assert.Equal(t, "http_request", entry.msg)
	assert.Equal(t, "GET", entry.fields["method"])
	assert.Equal(t, "/health", entry.fields["route"])
	assert.Equal(t, "203.0.113.5", entry.fields["remote_ip"])
	assert.EqualValues(t, http.StatusOK, entry.fields["status"])
}

func TestAccessLog_RedactsTokenFromConfirmPath(t *testing.T) {
	log := &recordingLogger{}

	r := chi.NewRouter()
	r.Use(middleware.AccessLog(log))
	r.Get("/api/confirm/{token}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/confirm/super-secret-bearer-token")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Len(t, log.entries, 1)
	logged := log.entries[0].fields["route"].(string)
	assert.NotContains(t, logged, "super-secret-bearer-token",
		"raw confirm token must not appear in logs")
	assert.Contains(t, logged, "/api/confirm/{token}")
}

func TestAccessLog_RedactsEmailQueryParam(t *testing.T) {
	log := &recordingLogger{}

	r := chi.NewRouter()
	r.Use(middleware.AccessLog(log))
	r.Get("/api/subscriptions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/subscriptions?email=alice%40example.com")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Len(t, log.entries, 1)
	logged := log.entries[0].fields["route"].(string)
	assert.NotContains(t, strings.ToLower(logged), "alice@example.com",
		"raw email PII must not appear in logs")
	assert.Equal(t, "/api/subscriptions", logged)
}

func TestAccessLog_RedactsSecretLikeUserAgent(t *testing.T) {
	log := &recordingLogger{}
	h := middleware.AccessLog(log)(accessOKHandler())

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	req.Header.Set("User-Agent", "Authorization: Bearer secret")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	require.Len(t, log.entries, 1)
	assert.Equal(t, "<redacted>", log.entries[0].fields["user_agent"])
}

func accessOKHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}
