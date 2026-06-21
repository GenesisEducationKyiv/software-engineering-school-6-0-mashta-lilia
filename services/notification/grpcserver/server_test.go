package grpcserver

import (
	"context"
	"errors"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/services/notification"
	"testing"

	notificationv1 "github-release-notifier/internal/gen/notification/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testConfirmURL = "https://app.example/api/confirm/tok-123"

type fakeService struct {
	confirmation *notification.Confirmation
	email        string
	repo         string
	release      *notification.ReleaseInfo
	calls        int
	delivered    bool
	err          error
}

func (f *fakeService) SendConfirmation(_ context.Context, c notification.Confirmation) (bool, error) {
	f.calls++
	f.confirmation = &c
	return f.delivered, f.err
}

func (f *fakeService) SendReleaseNotification(
	_ context.Context, email, repo string, rel *notification.ReleaseInfo,
) (bool, error) {
	f.calls++
	f.email, f.repo, f.release = email, repo, rel
	return f.delivered, f.err
}

func TestServer_SendReleaseNotification_MapsPopulatedRelease(t *testing.T) {
	t.Parallel()
	svc := &fakeService{delivered: true}
	srv := New(svc, logger.Nop())

	resp, err := srv.SendReleaseNotification(
		context.Background(),
		&notificationv1.SendReleaseNotificationRequest{
			Email: "alice@example.com",
			Repo:  "golang/go",
			Release: &notificationv1.Release{
				TagName:     "v1.22.0",
				Name:        "Go 1.22",
				HtmlUrl:     "https://github.com/golang/go/releases/tag/v1.22.0",
				PublishedAt: "2026-06-10T10:00:00Z",
			},
		},
	)

	require.NoError(t, err)
	assert.True(t, resp.GetDelivered())
	assert.Equal(t, "alice@example.com", svc.email)
	assert.Equal(t, "golang/go", svc.repo)
	require.NotNil(t, svc.release)
	assert.Equal(t, "v1.22.0", svc.release.TagName)
	assert.Equal(t, "Go 1.22", svc.release.Name)
	assert.Equal(t, "https://github.com/golang/go/releases/tag/v1.22.0", svc.release.HTMLURL)
	assert.Equal(t, "2026-06-10T10:00:00Z", svc.release.PublishedAt)
}

func TestServer_SendConfirmation_MapsFields(t *testing.T) {
	t.Parallel()
	svc := &fakeService{delivered: true}
	srv := New(svc, logger.Nop())

	resp, err := srv.SendConfirmation(context.Background(), &notificationv1.SendConfirmationRequest{
		Email:      "alice@example.com",
		ConfirmUrl: testConfirmURL,
		Repo:       "golang/go",
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDelivered())
	require.NotNil(t, svc.confirmation)
	assert.Equal(t, "alice@example.com", svc.confirmation.Email)
	assert.Equal(t, testConfirmURL, svc.confirmation.ConfirmURL)
	assert.Equal(t, "golang/go", svc.confirmation.Repo)
}

func TestServer_SendConfirmation_RejectsMissingFields(t *testing.T) {
	t.Parallel()
	cases := map[string]*notificationv1.SendConfirmationRequest{
		"nil request":         nil,
		"missing email":       {ConfirmUrl: testConfirmURL, Repo: "golang/go"},
		"missing confirm_url": {Email: "a@b.c", Repo: "golang/go"},
		"missing repo":        {Email: "a@b.c", ConfirmUrl: testConfirmURL},
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
		Email: "a@b.c", ConfirmUrl: testConfirmURL, Repo: "golang/go",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}
