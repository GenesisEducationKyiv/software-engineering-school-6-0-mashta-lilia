package notification

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github-release-notifier/internal/platform/tracectx"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	traceIDHexLen = 32 // W3C trace-id hex length
	spanIDBytes   = 8  // W3C parent-id: 8 bytes => 16 hex chars
)

// TraceUnaryClientInterceptor forwards the in-context trace id to the notifier as
// gRPC metadata so a request can be correlated across the service boundary.
func TraceUnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req any,
		reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		if traceID, ok := tracectx.FromContext(ctx); ok && traceID != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", traceID)
			if len(traceID) == traceIDHexLen {
				if spanID, err := newSpanID(); err == nil {
					ctx = metadata.AppendToOutgoingContext(
						ctx, "traceparent", "00-"+traceID+"-"+spanID+"-01",
					)
				}
			}
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// newSpanID returns a random 8-byte span id as 16 lowercase hex chars. The W3C
// traceparent format rejects an all-zero parent-id, so each call gets a fresh one.
func newSpanID() (string, error) {
	var b [spanIDBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
