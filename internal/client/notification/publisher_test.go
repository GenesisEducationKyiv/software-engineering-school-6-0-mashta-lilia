package notification_test

import (
	"context"
	"encoding/json"
	"errors"
	"github-release-notifier/internal/notifyevent"
	"github-release-notifier/internal/platform/tracectx"
	"github-release-notifier/internal/release"
	"testing"

	notificationclient "github-release-notifier/internal/client/notification"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBroker struct {
	routingKey string
	body       []byte
	calls      int
	err        error
}

func (f *fakeBroker) Publish(_ context.Context, routingKey string, body []byte) error {
	f.calls++
	f.routingKey = routingKey
	f.body = body
	return f.err
}

func newPublisher(t *testing.T, b *fakeBroker) *notificationclient.Publisher {
	t.Helper()
	p, err := notificationclient.NewPublisher(b, nil)
	require.NoError(t, err)
	return p
}

func decodePayload[T any](t *testing.T, body []byte, wantType string) T {
	t.Helper()
	var env notifyevent.Envelope
	require.NoError(t, json.Unmarshal(body, &env))
	assert.Equal(t, wantType, env.Type)
	var payload T
	require.NoError(t, json.Unmarshal(env.Payload, &payload))
	return payload
}

func TestSendConfirmation_PublishesCommand(t *testing.T) {
	b := &fakeBroker{}
	p := newPublisher(t, b)

	err := p.SendConfirmation(
		context.Background(), "user@example.com", "https://app.local/confirm/tok", "golang/go",
	)
	require.NoError(t, err)

	assert.Equal(t, 1, b.calls)
	assert.Equal(t, notifyevent.TypeConfirmation, b.routingKey)
	cmd := decodePayload[notifyevent.ConfirmationCommand](t, b.body, notifyevent.TypeConfirmation)
	assert.Equal(t, "user@example.com", cmd.Email)
	assert.Equal(t, "https://app.local/confirm/tok", cmd.ConfirmURL)
	assert.Equal(t, "golang/go", cmd.Repo)
}

func TestSendReleaseNotification_PublishesCommand(t *testing.T) {
	b := &fakeBroker{}
	p := newPublisher(t, b)

	rel := &release.Release{
		TagName:     "v1.2.3",
		Name:        "Release 1.2.3",
		HTMLURL:     "https://github.com/golang/go/releases/tag/v1.2.3",
		PublishedAt: "2026-06-21T00:00:00Z",
	}
	err := p.SendReleaseNotification(context.Background(), "user@example.com", "golang/go", rel)
	require.NoError(t, err)

	assert.Equal(t, notifyevent.TypeRelease, b.routingKey)
	cmd := decodePayload[notifyevent.ReleaseCommand](t, b.body, notifyevent.TypeRelease)
	assert.Equal(t, "user@example.com", cmd.Email)
	assert.Equal(t, "golang/go", cmd.Repo)
	assert.Equal(t, "v1.2.3", cmd.Release.TagName)
	assert.Equal(t, "https://github.com/golang/go/releases/tag/v1.2.3", cmd.Release.HTMLURL)
}

func TestSendReleaseNotification_NilReleaseDoesNotPanic(t *testing.T) {
	b := &fakeBroker{}
	p := newPublisher(t, b)

	err := p.SendReleaseNotification(context.Background(), "user@example.com", "golang/go", nil)
	require.NoError(t, err)

	cmd := decodePayload[notifyevent.ReleaseCommand](t, b.body, notifyevent.TypeRelease)
	assert.Equal(t, notifyevent.Release{}, cmd.Release)
}

func TestPublish_PropagatesTraceID(t *testing.T) {
	b := &fakeBroker{}
	p := newPublisher(t, b)

	ctx := tracectx.WithTraceID(context.Background(), "trace-xyz")
	require.NoError(t, p.SendConfirmation(ctx, "user@example.com", "https://app.local/c", "golang/go"))

	var env notifyevent.Envelope
	require.NoError(t, json.Unmarshal(b.body, &env))
	assert.Equal(t, "trace-xyz", env.TraceID)
}

func TestPublish_BrokerErrorIsReturned(t *testing.T) {
	b := &fakeBroker{err: errors.New("broker down")}
	p := newPublisher(t, b)

	err := p.SendConfirmation(context.Background(), "user@example.com", "https://app.local/c", "golang/go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broker down")
}

func TestNewPublisher_RejectsNilBroker(t *testing.T) {
	_, err := notificationclient.NewPublisher(nil, nil)
	require.Error(t, err)
}
