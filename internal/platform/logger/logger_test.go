package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github-release-notifier/internal/platform/tracectx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogger_EmitsJSONWithBaselineFields(t *testing.T) {
	buf := &bytes.Buffer{}
	log := newWithWriter(Config{Level: "debug", ServiceName: "test-service"}, buf)

	log.Info(context.Background(), "started", "requestID", "abc", "err", assert.AnError)

	entry := decodeEntry(t, buf)
	assert.NotEmpty(t, entry["timestamp"])
	assert.Equal(t, "info", entry["level"])
	assert.Equal(t, "started", entry["msg"])
	assert.Equal(t, "test-service", entry["service"])
	assert.Equal(t, "abc", entry["request_id"])
	assert.Contains(t, entry, "error")

	_, err := time.Parse(time.RFC3339Nano, entry["timestamp"].(string))
	require.NoError(t, err)
}

func TestLogger_InjectsTraceIDFromContext(t *testing.T) {
	buf := &bytes.Buffer{}
	log := newWithWriter(Config{Level: "debug", ServiceName: "test-service"}, buf)
	ctx := tracectx.WithTraceID(context.Background(), "trace-123")

	log.Debug(ctx, "cache_lookup", "repo", "owner/name")

	entry := decodeEntry(t, buf)
	assert.Equal(t, "debug", entry["level"])
	assert.Equal(t, "trace-123", entry["trace_id"])
}

func TestLogger_WithKeepsTraceHandler(t *testing.T) {
	buf := &bytes.Buffer{}
	log := newWithWriter(Config{Level: "debug", ServiceName: "test-service"}, buf).With("component", "worker")
	ctx := tracectx.WithTraceID(context.Background(), "trace-456")

	log.Warn(ctx, "retry", "attempt", 2)

	entry := decodeEntry(t, buf)
	assert.Equal(t, "worker", entry["component"])
	assert.Equal(t, "trace-456", entry["trace_id"])
}

func decodeEntry(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	return entry
}
