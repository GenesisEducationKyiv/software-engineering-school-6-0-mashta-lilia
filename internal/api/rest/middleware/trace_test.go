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

func TestTraceID_RejectsMalformedTraceparent(t *testing.T) {
	const validTraceID = "0af7651916cd43dd8448eb211c80319c"
	cases := map[string]string{
		"too few parts":      "00-" + validTraceID + "-b7ad6b7169203331",
		"too many parts":     "00-" + validTraceID + "-b7ad6b7169203331-01-extra",
		"non-hex version":    "zz-" + validTraceID + "-b7ad6b7169203331-01",
		"non-hex flags":      "00-" + validTraceID + "-b7ad6b7169203331-zz",
		"zero span ID":       "00-" + validTraceID + "-0000000000000000-01",
		"non-hex trace ID":   "00-" + "zz" + validTraceID[2:] + "-b7ad6b7169203331-01",
		"short trace ID":     "00-" + validTraceID[:31] + "-b7ad6b7169203331-01",
		"all-zero trace ID":  "00-00000000000000000000000000000000-b7ad6b7169203331-01",
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			var got string
			h := middleware.TraceID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got, _ = tracectx.FromContext(r.Context())
			}))
			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			req.Header.Set("Traceparent", header)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			require.NotEmpty(t, got, "should fall back to generated UUID, not bare value")
			assert.NotEqual(t, header, got, "malformed traceparent must not be trusted")
		})
	}
}
