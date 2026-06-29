package sagaparticipant

import (
	"context"
	"encoding/json"
	"errors"
	"github-release-notifier/internal/messaging"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/sagaevent"
	"github-release-notifier/services/notification"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockSender struct{ mock.Mock }

func (m *mockSender) SendConfirmation(ctx context.Context, c notification.Confirmation) (bool, error) {
	args := m.Called(ctx, c)
	return args.Bool(0), args.Error(1)
}

type mockPublisher struct{ mock.Mock }

func (m *mockPublisher) Publish(ctx context.Context, routingKey string, body []byte) error {
	return m.Called(ctx, routingKey, body).Error(0)
}

func commandBody(t *testing.T, payload sagaevent.SendConfirmationPayload) []byte {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	body, err := json.Marshal(sagaevent.Command{
		SagaID:  "saga-1",
		Type:    sagaevent.CmdSendConfirmation,
		Payload: raw,
	})
	require.NoError(t, err)
	return body
}

var validPayload = sagaevent.SendConfirmationPayload{
	Email:      "alice@example.com",
	Repo:       "golang/go",
	ConfirmURL: "https://app.example/api/confirm/tok",
}

func TestParticipant_Handle_Success_RepliesSent(t *testing.T) {
	t.Parallel()
	sender := &mockSender{}
	pub := &mockPublisher{}
	sender.On("SendConfirmation", mock.Anything, mock.Anything).Return(true, nil)
	pub.On("Publish", mock.Anything, sagaevent.ReplyConfirmationSent, mock.Anything).Return(nil)

	p, err := New(sender, pub, logger.Nop())
	require.NoError(t, err)

	assert.Equal(t, messaging.Ack, p.Handle(context.Background(), commandBody(t, validPayload)))
	pub.AssertCalled(t, "Publish", mock.Anything, sagaevent.ReplyConfirmationSent, mock.Anything)
}

// A redelivered command is deduped by the ledger (delivered=false, no error);
// that is still a "sent" outcome for the saga.
func TestParticipant_Handle_Deduped_RepliesSent(t *testing.T) {
	t.Parallel()
	sender := &mockSender{}
	pub := &mockPublisher{}
	sender.On("SendConfirmation", mock.Anything, mock.Anything).Return(false, nil)
	pub.On("Publish", mock.Anything, sagaevent.ReplyConfirmationSent, mock.Anything).Return(nil)

	p, err := New(sender, pub, logger.Nop())
	require.NoError(t, err)

	assert.Equal(t, messaging.Ack, p.Handle(context.Background(), commandBody(t, validPayload)))
	pub.AssertCalled(t, "Publish", mock.Anything, sagaevent.ReplyConfirmationSent, mock.Anything)
}

func TestParticipant_Handle_SendFailure_RepliesFailed(t *testing.T) {
	t.Parallel()
	sender := &mockSender{}
	pub := &mockPublisher{}
	sender.On("SendConfirmation", mock.Anything, mock.Anything).Return(false, errors.New("smtp down"))
	pub.On("Publish", mock.Anything, sagaevent.ReplyConfirmationFailed, mock.Anything).Return(nil)

	p, err := New(sender, pub, logger.Nop())
	require.NoError(t, err)

	assert.Equal(t, messaging.Ack, p.Handle(context.Background(), commandBody(t, validPayload)))
	pub.AssertCalled(t, "Publish", mock.Anything, sagaevent.ReplyConfirmationFailed, mock.Anything)
}

func TestParticipant_Handle_ReplyPublishFailure_Requeues(t *testing.T) {
	t.Parallel()
	sender := &mockSender{}
	pub := &mockPublisher{}
	sender.On("SendConfirmation", mock.Anything, mock.Anything).Return(true, nil)
	pub.On("Publish", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("broker down"))

	p, err := New(sender, pub, logger.Nop())
	require.NoError(t, err)

	assert.Equal(t, messaging.Requeue, p.Handle(context.Background(), commandBody(t, validPayload)))
}

func TestParticipant_Handle_InvalidCommand_RepliesFailed(t *testing.T) {
	t.Parallel()
	sender := &mockSender{}
	pub := &mockPublisher{}
	pub.On("Publish", mock.Anything, sagaevent.ReplyConfirmationFailed, mock.Anything).Return(nil)

	p, err := New(sender, pub, logger.Nop())
	require.NoError(t, err)

	body := commandBody(t, sagaevent.SendConfirmationPayload{Email: "", Repo: "", ConfirmURL: ""})
	assert.Equal(t, messaging.Ack, p.Handle(context.Background(), body))
	sender.AssertNotCalled(t, "SendConfirmation", mock.Anything, mock.Anything)
	pub.AssertCalled(t, "Publish", mock.Anything, sagaevent.ReplyConfirmationFailed, mock.Anything)
}

func TestParticipant_Handle_UnknownType_Drops(t *testing.T) {
	t.Parallel()
	p, err := New(&mockSender{}, &mockPublisher{}, logger.Nop())
	require.NoError(t, err)

	body, err := json.Marshal(sagaevent.Command{SagaID: "saga-1", Type: "saga.other"})
	require.NoError(t, err)
	assert.Equal(t, messaging.Drop, p.Handle(context.Background(), body))
}
