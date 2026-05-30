package middleware

import (
	"net/http"
	"strings"

	"github-release-notifier/internal/platform/tracectx"

	"github.com/google/uuid"
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
	if traceID := parseTraceparent(r.Header.Get(headerTraceparent)); traceID != "" {
		return traceID
	}
	if requestID := strings.TrimSpace(r.Header.Get(headerRequestID)); requestID != "" {
		return requestID
	}
	return uuid.NewString()
}

func parseTraceparent(header string) string {
	parts := strings.Split(strings.TrimSpace(header), "-")
	if len(parts) < 4 {
		return ""
	}
	traceID := strings.ToLower(parts[1])
	if len(traceID) != 32 || traceID == "00000000000000000000000000000000" {
		return ""
	}
	for _, r := range traceID {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return traceID
}
