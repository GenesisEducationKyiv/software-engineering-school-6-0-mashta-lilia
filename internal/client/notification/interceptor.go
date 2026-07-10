package notification

import (
	"context"
	"github-release-notifier/internal/platform/tracectx"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
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
			if traceparent, ok := tracectx.Traceparent(traceID); ok {
				ctx = metadata.AppendToOutgoingContext(ctx, "traceparent", traceparent)
			}
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
