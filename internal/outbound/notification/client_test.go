package notification_test

import (
	"context"
	"github-release-notifier/internal/outbound/notification"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/platform/tracectx"
	"github-release-notifier/internal/release"
	"net"
	"testing"

	notificationv1 "github-release-notifier/internal/gen/notification/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

type recordingServer struct {
	notificationv1.UnimplementedNotificationServiceServer
	confirmationReq *notificationv1.SendConfirmationRequest
	releaseReq      *notificationv1.SendReleaseNotificationRequest
	response        *notificationv1.SendNotificationResponse
	err             error
	traceID         string
	traceparent     string
}

func (s *recordingServer) SendConfirmation(
	ctx context.Context,
	req *notificationv1.SendConfirmationRequest,
) (*notificationv1.SendNotificationResponse, error) {
	s.confirmationReq = req
	s.captureTrace(ctx)
	if s.err != nil {
		return nil, s.err
	}
	return s.responseOrDefault(), nil
}

func (s *recordingServer) SendReleaseNotification(
	ctx context.Context,
	req *notificationv1.SendReleaseNotificationRequest,
) (*notificationv1.SendNotificationResponse, error) {
	s.releaseReq = req
	s.captureTrace(ctx)
	if s.err != nil {
		return nil, s.err
	}
	return s.responseOrDefault(), nil
}

func (s *recordingServer) responseOrDefault() *notificationv1.SendNotificationResponse {
	if s.response != nil {
		return s.response
	}
	return &notificationv1.SendNotificationResponse{Delivered: true}
}

func (s *recordingServer) captureTrace(ctx context.Context) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return
	}
	if ids := md.Get("x-request-id"); len(ids) > 0 {
		s.traceID = ids[0]
	}
	if parents := md.Get("traceparent"); len(parents) > 0 {
		s.traceparent = parents[0]
	}
}

func TestClient_SendConfirmationMapsRequest(t *testing.T) {
	t.Parallel()
	srv := &recordingServer{}
	client := newTestClient(t, srv)

	err := client.SendConfirmation(context.Background(), "alice@example.com", "tok", "golang/go")

	require.NoError(t, err)
	require.NotNil(t, srv.confirmationReq)
	assert.Equal(t, "alice@example.com", srv.confirmationReq.GetEmail())
	assert.Equal(t, "tok", srv.confirmationReq.GetToken())
	assert.Equal(t, "golang/go", srv.confirmationReq.GetRepo())
}

func TestClient_SendReleaseNotificationMapsRequest(t *testing.T) {
	t.Parallel()
	srv := &recordingServer{}
	client := newTestClient(t, srv)

	err := client.SendReleaseNotification(context.Background(), "alice@example.com", "golang/go", &release.Release{
		TagName:     "v1.22.0",
		Name:        "Go 1.22",
		HTMLURL:     "https://github.com/golang/go/releases/tag/v1.22.0",
		PublishedAt: "2026-06-10T10:00:00Z",
	})

	require.NoError(t, err)
	require.NotNil(t, srv.releaseReq)
	assert.Equal(t, "alice@example.com", srv.releaseReq.GetEmail())
	assert.Equal(t, "golang/go", srv.releaseReq.GetRepo())
	require.NotNil(t, srv.releaseReq.GetRelease())
	assert.Equal(t, "v1.22.0", srv.releaseReq.GetRelease().GetTagName())
	assert.Equal(t, "Go 1.22", srv.releaseReq.GetRelease().GetName())
	assert.Equal(t, "https://github.com/golang/go/releases/tag/v1.22.0", srv.releaseReq.GetRelease().GetHtmlUrl())
	assert.Equal(t, "2026-06-10T10:00:00Z", srv.releaseReq.GetRelease().GetPublishedAt())
}

func TestClient_DeliveredFalseIsBusinessNoop(t *testing.T) {
	t.Parallel()
	srv := &recordingServer{response: &notificationv1.SendNotificationResponse{Delivered: false}}
	client := newTestClient(t, srv)

	err := client.SendConfirmation(context.Background(), "alice@example.com", "tok", "golang/go")

	assert.NoError(t, err)
}

func TestClient_TransportErrorIsReturned(t *testing.T) {
	t.Parallel()
	srv := &recordingServer{err: status.Error(codes.Unavailable, "notifier down")}
	client := newTestClient(t, srv)

	err := client.SendConfirmation(context.Background(), "alice@example.com", "tok", "golang/go")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "code=Unavailable")
}

func TestClient_PropagatesTraceMetadata(t *testing.T) {
	t.Parallel()
	srv := &recordingServer{}
	client := newTestClient(t, srv)
	ctx := tracectx.WithTraceID(context.Background(), "1234567890abcdef1234567890abcdef")

	err := client.SendConfirmation(ctx, "alice@example.com", "tok", "golang/go")

	require.NoError(t, err)
	assert.Equal(t, "1234567890abcdef1234567890abcdef", srv.traceID)
	assert.Equal(t, "00-1234567890abcdef1234567890abcdef-0000000000000000-01", srv.traceparent)
}

func newTestClient(t *testing.T, srv notificationv1.NotificationServiceServer) *notification.Client {
	t.Helper()
	listener := bufconn.Listen(bufSize)
	server := grpc.NewServer()
	notificationv1.RegisterNotificationServiceServer(server, srv)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(notification.TraceUnaryClientInterceptor()),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = conn.Close()
	})

	client, err := notification.NewClient(notificationv1.NewNotificationServiceClient(conn), logger.Nop())
	require.NoError(t, err)
	return client
}
