package tracectx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type contextKey string

const traceIDKey contextKey = "trace_id"

const (
	traceIDBytes  = 16 // W3C trace-id is 16 bytes => 32 hex chars
	spanIDBytes   = 8  // W3C parent-id is 8 bytes => 16 hex chars
	traceIDHexLen = 2 * traceIDBytes
	zeroTraceID   = "00000000000000000000000000000000"
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
