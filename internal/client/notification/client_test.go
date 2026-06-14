package notification_test

import (
	"context"
	"github-release-notifier/internal/client/notification"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/release"
	"testing"

	notificationv1 "github-release-notifier/internal/gen/notification/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// mockServiceClient is injected directly into the Client, so the client logic is
// tested without standing up a gRPC server. Trace propagation is covered
// separately in interceptor_test.go.
type mockServiceClient struct {
	mock.Mock
}

func (m *mockServiceClient) SendConfirmation(
	ctx context.Context, in *notificationv1.SendConfirmationRequest, _ ...grpc.CallOption,
) (*notificationv1.SendNotificationResponse, error) {
	args := m.Called(ctx, in)
	resp, _ := args.Get(0).(*notificationv1.SendNotificationResponse)
	return resp, args.Error(1)
}

func (m *mockServiceClient) SendReleaseNotification(
	ctx context.Context, in *notificationv1.SendReleaseNotificationRequest, _ ...grpc.CallOption,
) (*notificationv1.SendNotificationResponse, error) {
	args := m.Called(ctx, in)
	resp, _ := args.Get(0).(*notificationv1.SendNotificationResponse)
	return resp, args.Error(1)
}

func delivered() *notificationv1.SendNotificationResponse {
	return &notificationv1.SendNotificationResponse{Delivered: true}
}

func TestClient_SendConfirmationMapsRequest(t *testing.T) {
	t.Parallel()
	m := &mockServiceClient{}
	var got *notificationv1.SendConfirmationRequest
	m.On("SendConfirmation", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			got, _ = args.Get(1).(*notificationv1.SendConfirmationRequest)
		}).
		Return(delivered(), nil)

	client, err := notification.NewClient(m, logger.Nop())
	require.NoError(t, err)

	require.NoError(t, client.SendConfirmation(context.Background(), "alice@example.com", "tok", "golang/go"))
	require.NotNil(t, got)
	assert.Equal(t, "alice@example.com", got.GetEmail())
	assert.Equal(t, "tok", got.GetToken())
	assert.Equal(t, "golang/go", got.GetRepo())
	m.AssertExpectations(t)
}

func TestClient_SendReleaseNotificationMapsRequest(t *testing.T) {
	t.Parallel()
	m := &mockServiceClient{}
	var got *notificationv1.SendReleaseNotificationRequest
	m.On("SendReleaseNotification", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			got, _ = args.Get(1).(*notificationv1.SendReleaseNotificationRequest)
		}).
		Return(delivered(), nil)

	client, err := notification.NewClient(m, logger.Nop())
	require.NoError(t, err)

	err = client.SendReleaseNotification(context.Background(), "alice@example.com", "golang/go", &release.Release{
		TagName:     "v1.22.0",
		Name:        "Go 1.22",
		HTMLURL:     "https://github.com/golang/go/releases/tag/v1.22.0",
		PublishedAt: "2026-06-10T10:00:00Z",
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "alice@example.com", got.GetEmail())
	assert.Equal(t, "golang/go", got.GetRepo())
	require.NotNil(t, got.GetRelease())
	assert.Equal(t, "v1.22.0", got.GetRelease().GetTagName())
	assert.Equal(t, "Go 1.22", got.GetRelease().GetName())
	assert.Equal(t, "https://github.com/golang/go/releases/tag/v1.22.0", got.GetRelease().GetHtmlUrl())
	assert.Equal(t, "2026-06-10T10:00:00Z", got.GetRelease().GetPublishedAt())
	m.AssertExpectations(t)
}

func TestClient_DeliveredFalseIsBusinessNoop(t *testing.T) {
	t.Parallel()
	m := &mockServiceClient{}
	m.On("SendConfirmation", mock.Anything, mock.Anything).
		Return(&notificationv1.SendNotificationResponse{Delivered: false}, nil)

	client, err := notification.NewClient(m, logger.Nop())
	require.NoError(t, err)

	assert.NoError(t, client.SendConfirmation(context.Background(), "alice@example.com", "tok", "golang/go"))
}

func TestClient_TransportErrorIsReturned(t *testing.T) {
	t.Parallel()
	m := &mockServiceClient{}
	m.On("SendConfirmation", mock.Anything, mock.Anything).
		Return((*notificationv1.SendNotificationResponse)(nil), status.Error(codes.Unavailable, "notifier down"))

	client, err := notification.NewClient(m, logger.Nop())
	require.NoError(t, err)

	err = client.SendConfirmation(context.Background(), "alice@example.com", "tok", "golang/go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "code=Unavailable")
}
