package middleware

import (
	"github-release-notifier/internal/platform/tracectx"
	"net/http"
	"strings"
)

const (
	headerTraceparent = "Traceparent"
	headerRequestID   = "X-Request-ID"
)

func TraceID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := traceIDFromRequest(r)
		w.Header().Set(headerRequestID, traceID)
		next.ServeHTTP(w, r.WithContext(tracectx.WithTraceID(r.Context(), traceID)))
	})
}

func traceIDFromRequest(r *http.Request) string {
	if traceID, ok := tracectx.ParseTraceparent(r.Header.Get(headerTraceparent)); ok {
		return traceID
	}
	if requestID := strings.TrimSpace(r.Header.Get(headerRequestID)); tracectx.IsSafeExternalID(requestID) {
		return requestID
	}
	return tracectx.NewID()
}
