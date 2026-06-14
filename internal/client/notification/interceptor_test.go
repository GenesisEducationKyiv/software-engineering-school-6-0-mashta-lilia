package notification_test

import (
	"context"
	"github-release-notifier/internal/client/notification"
	"github-release-notifier/internal/platform/tracectx"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestTraceUnaryClientInterceptor_PropagatesTraceMetadata(t *testing.T) {
	t.Parallel()
	interceptor := notification.TraceUnaryClientInterceptor()
	ctx := tracectx.WithTraceID(context.Background(), "1234567890abcdef1234567890abcdef")

	var got metadata.MD
	invoker := func(
		ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption,
	) error {
		got, _ = metadata.FromOutgoingContext(ctx)
		return nil
	}

	require.NoError(t, interceptor(ctx, "/test.Method", nil, nil, nil, invoker))
	assert.Equal(t, []string{"1234567890abcdef1234567890abcdef"}, got.Get("x-request-id"))
	assert.Equal(t,
		[]string{"00-1234567890abcdef1234567890abcdef-0000000000000000-01"},
		got.Get("traceparent"),
	)
}

func TestTraceUnaryClientInterceptor_WithoutTraceIDAddsNoMetadata(t *testing.T) {
	t.Parallel()
	interceptor := notification.TraceUnaryClientInterceptor()

	called := false
	invoker := func(
		ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption,
	) error {
		called = true
		md, _ := metadata.FromOutgoingContext(ctx)
		assert.Empty(t, md.Get("x-request-id"))
		assert.Empty(t, md.Get("traceparent"))
		return nil
	}

	require.NoError(t, interceptor(context.Background(), "/test.Method", nil, nil, nil, invoker))
	assert.True(t, called)
}
