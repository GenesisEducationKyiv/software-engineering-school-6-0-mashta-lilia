package middleware

import (
	"github-release-notifier/internal/platform/tracectx"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const (
	headerTraceparent = "Traceparent"
	headerRequestID   = "X-Request-ID"
	traceparentParts  = 4
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
	if requestID := strings.TrimSpace(r.Header.Get(headerRequestID)); isSafeRequestID(requestID) {
		return requestID
	}
	return uuid.NewString()
}

const maxRequestIDLength = 64

// Unsafe header bytes must not reach logs or gRPC metadata, which rejects them and fails the RPC.
func isSafeRequestID(s string) bool {
	if s == "" || len(s) > maxRequestIDLength {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

func parseTraceparent(header string) string {
	trimmed := strings.TrimSpace(header)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "-")
	if len(parts) != traceparentParts {
		return ""
	}
	if len(parts[0]) != 2 || !isHex(parts[0]) {
		return ""
	}
	spanID := strings.ToLower(parts[2])
	if len(spanID) != 16 || !isHex(spanID) || spanID == "0000000000000000" {
		return ""
	}
	if len(parts[3]) != 2 || !isHex(parts[3]) {
		return ""
	}
	traceID := strings.ToLower(parts[1])
	if len(traceID) != 32 || traceID == "00000000000000000000000000000000" || !isHex(traceID) {
		return ""
	}
	return traceID
}

func isHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
