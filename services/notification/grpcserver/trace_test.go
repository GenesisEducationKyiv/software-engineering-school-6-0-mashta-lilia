package grpcserver

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
)

func ctxWithMetadata(md metadata.MD) context.Context {
	return metadata.NewIncomingContext(context.Background(), md)
}

func TestTraceIDFromMetadata_ValidTraceparent(t *testing.T) {
	t.Parallel()
	ctx := ctxWithMetadata(metadata.Pairs(
		"traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
	))
	assert.Equal(t, "0af7651916cd43dd8448eb211c80319c", traceIDFromMetadata(ctx))
}

func TestTraceIDFromMetadata_RejectsInvalidTraceparent(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"all-zero trace id":  "00-00000000000000000000000000000000-b7ad6b7169203331-01",
		"non-hex trace id":   "00-zzf7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
		"short trace id":     "00-0af7651916cd43dd8448eb211c80319-b7ad6b7169203331-01",
		"all-zero parent id": "00-0af7651916cd43dd8448eb211c80319c-0000000000000000-01",
		"too few parts":      "00-0af7651916cd43dd8448eb211c80319c-01",
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := ctxWithMetadata(metadata.Pairs("traceparent", header))
			assert.Empty(t, traceIDFromMetadata(ctx))
		})
	}
}

func TestTraceIDFromMetadata_FallsBackToSafeRequestID(t *testing.T) {
	t.Parallel()
	ctx := ctxWithMetadata(metadata.Pairs("x-request-id", "abc-123_safe.id"))
	assert.Equal(t, "abc-123_safe.id", traceIDFromMetadata(ctx))
}

func TestTraceIDFromMetadata_RejectsUnsafeRequestID(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"contains newline": "abc\ndef",
		"contains space":   "abc def",
		"too long":         strings.Repeat("a", 65),
	}
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := ctxWithMetadata(metadata.Pairs("x-request-id", id))
			assert.Empty(t, traceIDFromMetadata(ctx))
		})
	}
}

func TestTraceIDFromMetadata_NoMetadata(t *testing.T) {
	t.Parallel()
	assert.Empty(t, traceIDFromMetadata(context.Background()))
}
