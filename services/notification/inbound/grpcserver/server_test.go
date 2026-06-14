package grpcserver

import (
	"context"
	"errors"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/services/notification/model"
	"testing"

	notificationv1 "github-release-notifier/internal/gen/notification/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeService struct {
	confirmation *model.Confirmation
	email        string
	repo         string
	release      *model.ReleaseInfo
	calls        int
	delivered    bool
	err          error
}

func (f *fakeService) SendConfirmation(_ context.Context, c model.Confirmation) (bool, error) {
	f.calls++
	f.confirmation = &c
	return f.delivered, f.err
}

func (f *fakeService) SendReleaseNotification(
	_ context.Context, email, repo string, rel *model.ReleaseInfo,
) (bool, error) {
	f.calls++
	f.email, f.repo, f.release = email, repo, rel
	return f.delivered, f.err
}

func TestServer_SendConfirmation_MapsFields(t *testing.T) {
	t.Parallel()
	svc := &fakeService{delivered: true}
	srv := New(svc, logger.Nop())

	resp, err := srv.SendConfirmation(context.Background(), &notificationv1.SendConfirmationRequest{
		Email: "alice@example.com",
		Token: "tok-123",
		Repo:  "golang/go",
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDelivered())
	require.NotNil(t, svc.confirmation)
	assert.Equal(t, "alice@example.com", svc.confirmation.Email)
	assert.Equal(t, "tok-123", svc.confirmation.Token)
	assert.Equal(t, "golang/go", svc.confirmation.Repo)
}

func TestServer_SendConfirmation_RejectsMissingFields(t *testing.T) {
	t.Parallel()
	cases := map[string]*notificationv1.SendConfirmationRequest{
		"nil request":   nil,
		"missing email": {Token: "tok", Repo: "golang/go"},
		"missing token": {Email: "a@b.c", Repo: "golang/go"},
		"missing repo":  {Email: "a@b.c", Token: "tok"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeService{delivered: true}
			srv := New(svc, logger.Nop())

			resp, err := srv.SendConfirmation(context.Background(), req)

			assert.Nil(t, resp)
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
			assert.Zero(t, svc.calls, "service must not be called for invalid input")
		})
	}
}

func TestServer_SendReleaseNotification_RejectsMissingFields(t *testing.T) {
	t.Parallel()
	cases := map[string]*notificationv1.SendReleaseNotificationRequest{
		"nil request":   nil,
		"missing email": {Repo: "golang/go"},
		"missing repo":  {Email: "a@b.c"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeService{delivered: true}
			srv := New(svc, logger.Nop())

			resp, err := srv.SendReleaseNotification(context.Background(), req)

			assert.Nil(t, resp)
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
			assert.Zero(t, svc.calls, "service must not be called for invalid input")
		})
	}
}

func TestServer_SendReleaseNotification_NilReleaseAllowed(t *testing.T) {
	t.Parallel()
	svc := &fakeService{delivered: true}
	srv := New(svc, logger.Nop())

	resp, err := srv.SendReleaseNotification(
		context.Background(),
		&notificationv1.SendReleaseNotificationRequest{Email: "a@b.c", Repo: "golang/go"},
	)

	require.NoError(t, err)
	assert.True(t, resp.GetDelivered())
	assert.Nil(t, svc.release)
}

func TestServer_ServiceErrorMapsToInternal(t *testing.T) {
	t.Parallel()
	svc := &fakeService{err: errors.New("smtp down")}
	srv := New(svc, logger.Nop())

	resp, err := srv.SendConfirmation(context.Background(), &notificationv1.SendConfirmationRequest{
		Email: "a@b.c", Token: "tok", Repo: "golang/go",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}
