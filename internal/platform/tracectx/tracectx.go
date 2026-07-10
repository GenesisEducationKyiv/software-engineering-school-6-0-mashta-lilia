package tracectx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

type contextKey string

const traceIDKey contextKey = "trace_id"

const (
	traceIDBytes     = 16 // W3C trace-id is 16 bytes => 32 hex chars
	spanIDBytes      = 8  // W3C parent-id is 8 bytes => 16 hex chars
	traceIDHexLen    = 2 * traceIDBytes
	spanIDHexLen     = 2 * spanIDBytes
	zeroTraceID      = "00000000000000000000000000000000"
	zeroSpanID       = "0000000000000000"
	traceparentParts = 4
	maxExternalIDLen = 64
)

func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceIDKey, id)
}

func FromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, ok := ctx.Value(traceIDKey).(string)
	return v, ok
}

// NewID returns a fresh 32-hex-char W3C trace id. Trace ids are correlation
// handles, not secrets.
func NewID() string {
	var b [traceIDBytes]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read cannot fail (Go 1.24+)
	return hex.EncodeToString(b[:])
}

// IsValidID reports whether id is a non-zero, 32-char lowercase-hex W3C trace id.
func IsValidID(id string) bool {
	return len(id) == traceIDHexLen && id != zeroTraceID && isHexLower(id)
}

// Traceparent builds a W3C traceparent for traceID with a fresh random parent
// id, so the all-zero parent-id the spec rejects is never produced.
func Traceparent(traceID string) (string, bool) {
	if !IsValidID(traceID) {
		return "", false
	}
	for {
		var span [spanIDBytes]byte
		_, _ = rand.Read(span[:]) // crypto/rand.Read cannot fail (Go 1.24+)
		// Retry on the 1/2^64 all-zero draw the W3C spec rejects as a parent id.
		if span != ([spanIDBytes]byte{}) {
			return "00-" + traceID + "-" + hex.EncodeToString(span[:]) + "-01", true
		}
	}
}

func isHexLower(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// IsSafeExternalID reports whether s is safe to trust as a caller-supplied
// correlation id (an HTTP X-Request-ID or a gRPC x-request-id metadata value):
// bounded length and a conservative charset, so it can't inject control
// characters into logs or break gRPC metadata encoding.
func IsSafeExternalID(s string) bool {
	if s == "" || len(s) > maxExternalIDLen {
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

// ParseTraceparent extracts and validates the trace-id field from a W3C
// traceparent header/metadata value. It is the single validation path shared
// by the HTTP and gRPC transports, so a malformed or degenerate traceparent
// (bad version/flags, non-hex or all-zero trace-id/parent-id) is rejected
// identically on both sides instead of drifting.
func ParseTraceparent(header string) (string, bool) {
	trimmed := strings.TrimSpace(header)
	if trimmed == "" {
		return "", false
	}
	parts := strings.Split(trimmed, "-")
	if len(parts) != traceparentParts {
		return "", false
	}
	if len(parts[0]) != 2 || !isHexLower(strings.ToLower(parts[0])) {
		return "", false
	}
	spanID := strings.ToLower(parts[2])
	if len(spanID) != spanIDHexLen || !isHexLower(spanID) || spanID == zeroSpanID {
		return "", false
	}
	if len(parts[3]) != 2 || !isHexLower(strings.ToLower(parts[3])) {
		return "", false
	}
	traceID := strings.ToLower(parts[1])
	if !IsValidID(traceID) {
		return "", false
	}
	return traceID, true
}
