package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github-release-notifier/internal/api/rest/middleware"
	"github-release-notifier/internal/platform/tracectx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTraceID_UsesTraceparentTraceID(t *testing.T) {
	const traceID = "0af7651916cd43dd8448eb211c80319c"
	var got string
	h := middleware.TraceID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = tracectx.FromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Traceparent", "00-"+traceID+"-b7ad6b7169203331-01")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, traceID, got)
	assert.Equal(t, traceID, rec.Header().Get("X-Request-ID"))
}

func TestTraceID_FallsBackToRequestID(t *testing.T) {
	const requestID = "request-123"
	var got string
	h := middleware.TraceID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = tracectx.FromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("X-Request-ID", requestID)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, requestID, got)
	assert.Equal(t, requestID, rec.Header().Get("X-Request-ID"))
}

func TestTraceID_GeneratesID(t *testing.T) {
	var got string
	h := middleware.TraceID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = tracectx.FromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	require.NotEmpty(t, got)
	assert.Equal(t, got, rec.Header().Get("X-Request-ID"))
}
