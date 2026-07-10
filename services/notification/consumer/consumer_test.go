package consumer_test

import (
	"context"
	"encoding/json"
	"errors"
	"github-release-notifier/internal/messaging"
	"github-release-notifier/internal/notifyevent"
	"github-release-notifier/internal/platform/tracectx"
	"github-release-notifier/services/notification"
	"github-release-notifier/services/notification/consumer"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDispatcher struct {
	confirmation      notification.Confirmation
	confirmationCalls int

	releaseEmail string
	releaseRepo  string
	release      *notification.ReleaseInfo
	releaseCalls int

	capturedCtx context.Context
	err         error
}

func (f *fakeDispatcher) SendConfirmation(
	ctx context.Context, c notification.Confirmation,
) (bool, error) {
	f.confirmationCalls++
	f.confirmation = c
	f.capturedCtx = ctx
	if f.err != nil {
		return false, f.err
	}
	return true, nil
}

func (f *fakeDispatcher) SendReleaseNotification(
	ctx context.Context, email, repo string, rel *notification.ReleaseInfo,
) (bool, error) {
	f.releaseCalls++
	f.releaseEmail, f.releaseRepo, f.release = email, repo, rel
	f.capturedCtx = ctx
	if f.err != nil {
		return false, f.err
	}
	return true, nil
}

func mustEnvelope(t *testing.T, msgType, traceID string, payload any) []byte {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	body, err := json.Marshal(notifyevent.Envelope{
		Type:    msgType,
		TraceID: traceID,
		Payload: raw,
	})
	require.NoError(t, err)
	return body
}

func newConsumer(t *testing.T, d *fakeDispatcher) *consumer.Consumer {
	t.Helper()
	c, err := consumer.New(d, nil)
	require.NoError(t, err)
	return c
}

func TestHandle_Confirmation_DispatchesAndAcks(t *testing.T) {
	d := &fakeDispatcher{}
	c := newConsumer(t, d)

	body := mustEnvelope(t, notifyevent.TypeConfirmation, "", notifyevent.ConfirmationCommand{
		Email:      "user@example.com",
		ConfirmURL: "https://app.local/confirm/tok",
		Repo:       "golang/go",
	})

	action := c.Handle(context.Background(), body)

	assert.Equal(t, messaging.Ack, action)
	assert.Equal(t, 1, d.confirmationCalls)
	assert.Equal(t, notification.Confirmation{
		Email:      "user@example.com",
		ConfirmURL: "https://app.local/confirm/tok",
		Repo:       "golang/go",
	}, d.confirmation)
}

func TestHandle_Release_DispatchesAndAcks(t *testing.T) {
	d := &fakeDispatcher{}
	c := newConsumer(t, d)

	body := mustEnvelope(t, notifyevent.TypeRelease, "", notifyevent.ReleaseCommand{
		Email: "user@example.com",
		Repo:  "golang/go",
		Release: notifyevent.Release{
			TagName:     "v1.2.3",
			Name:        "Release 1.2.3",
			HTMLURL:     "https://github.com/golang/go/releases/tag/v1.2.3",
			PublishedAt: "2026-06-21T00:00:00Z",
		},
	})

	action := c.Handle(context.Background(), body)

	assert.Equal(t, messaging.Ack, action)
	require.Equal(t, 1, d.releaseCalls)
	assert.Equal(t, "user@example.com", d.releaseEmail)
	assert.Equal(t, "golang/go", d.releaseRepo)
	require.NotNil(t, d.release)
	assert.Equal(t, "v1.2.3", d.release.TagName)
	assert.Equal(t, "https://github.com/golang/go/releases/tag/v1.2.3", d.release.HTMLURL)
}

func TestHandle_PropagatesTraceID(t *testing.T) {
	d := &fakeDispatcher{}
	c := newConsumer(t, d)

	body := mustEnvelope(t, notifyevent.TypeConfirmation, "trace-xyz", notifyevent.ConfirmationCommand{
		Email:      "user@example.com",
		ConfirmURL: "https://app.local/confirm/tok",
		Repo:       "golang/go",
	})

	c.Handle(context.Background(), body)

	require.NotNil(t, d.capturedCtx)
	got, ok := tracectx.FromContext(d.capturedCtx)
	assert.True(t, ok)
	assert.Equal(t, "trace-xyz", got)
}

func TestHandle_TransientSendFailure_Requeues(t *testing.T) {
	d := &fakeDispatcher{err: errors.New("smtp unavailable")}
	c := newConsumer(t, d)

	body := mustEnvelope(t, notifyevent.TypeRelease, "", notifyevent.ReleaseCommand{
		Email:   "user@example.com",
		Repo:    "golang/go",
		Release: notifyevent.Release{TagName: "v1.2.3"},
	})

	action := c.Handle(context.Background(), body)

	assert.Equal(t, messaging.Requeue, action)
	assert.Equal(t, 1, d.releaseCalls)
}

func TestHandle_DropsBadInputWithoutDispatch(t *testing.T) {
	cases := map[string][]byte{
		"malformed envelope": []byte("{not-json"),
		"unknown type":       mustEnvelope(t, "bogus", "", map[string]string{"x": "y"}),
		"confirmation missing email": mustEnvelope(t, notifyevent.TypeConfirmation, "",
			notifyevent.ConfirmationCommand{ConfirmURL: "https://app.local/c", Repo: "golang/go"}),
		"confirmation missing url": mustEnvelope(t, notifyevent.TypeConfirmation, "",
			notifyevent.ConfirmationCommand{Email: "user@example.com", Repo: "golang/go"}),
		"release missing repo": mustEnvelope(t, notifyevent.TypeRelease, "",
			notifyevent.ReleaseCommand{Email: "user@example.com"}),
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			d := &fakeDispatcher{}
			c := newConsumer(t, d)

			action := c.Handle(context.Background(), body)

			assert.Equal(t, messaging.Drop, action)
			assert.Zero(t, d.confirmationCalls)
			assert.Zero(t, d.releaseCalls)
		})
	}
}

func TestHandle_DropsMalformedPayload(t *testing.T) {
	d := &fakeDispatcher{}
	c := newConsumer(t, d)

	// Valid envelope, but the payload is a JSON string where an object is expected.
	body, err := json.Marshal(notifyevent.Envelope{
		Type:    notifyevent.TypeConfirmation,
		Payload: json.RawMessage(`"not-an-object"`),
	})
	require.NoError(t, err)

	action := c.Handle(context.Background(), body)

	assert.Equal(t, messaging.Drop, action)
	assert.Zero(t, d.confirmationCalls)
}

func TestNew_RejectsNilDispatcher(t *testing.T) {
	_, err := consumer.New(nil, nil)
	require.Error(t, err)
}
