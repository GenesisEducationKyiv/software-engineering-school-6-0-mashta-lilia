package saga

import (
	"context"
	"encoding/json"
	"errors"
	"github-release-notifier/internal/messaging"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/sagaevent"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockStore struct{ mock.Mock }

func (m *mockStore) Create(ctx context.Context, in Instance) error {
	return m.Called(ctx, in).Error(0)
}

func (m *mockStore) Get(ctx context.Context, id string) (*Instance, error) {
	args := m.Called(ctx, id)
	inst, _ := args.Get(0).(*Instance)
	return inst, args.Error(1)
}

func (m *mockStore) CompleteIfAwaiting(ctx context.Context, id string) (bool, error) {
	args := m.Called(ctx, id)
	return args.Bool(0), args.Error(1)
}

func (m *mockStore) ClaimCompensation(ctx context.Context, id string) (SubscriptionData, bool, error) {
	args := m.Called(ctx, id)
	data, _ := args.Get(0).(SubscriptionData)
	return data, args.Bool(1), args.Error(2)
}

func (m *mockStore) MarkCompensated(ctx context.Context, id string) error {
	return m.Called(ctx, id).Error(0)
}

func (m *mockStore) ListTimedOut(ctx context.Context, now time.Time, limit int) ([]string, error) {
	args := m.Called(ctx, now, limit)
	ids, _ := args.Get(0).([]string)
	return ids, args.Error(1)
}

type mockPublisher struct{ mock.Mock }

func (m *mockPublisher) Publish(ctx context.Context, routingKey string, body []byte) error {
	return m.Called(ctx, routingKey, body).Error(0)
}

type mockCanceller struct{ mock.Mock }

func (m *mockCanceller) Cancel(ctx context.Context, subscriptionID int64) error {
	return m.Called(ctx, subscriptionID).Error(0)
}

func replyBody(t *testing.T, sagaID, replyType string) []byte {
	t.Helper()
	body, err := json.Marshal(sagaevent.Reply{SagaID: sagaID, Type: replyType})
	require.NoError(t, err)
	return body
}

// captureSagaID records the generated saga id from the Publish call, which runs
// after the waiter is registered, so a reply delivered afterwards is never lost.
func captureSagaID(pub *mockPublisher, idCh chan<- string) {
	pub.On("Publish", mock.Anything, sagaevent.CmdSendConfirmation, mock.Anything).Return(nil).
		Run(func(args mock.Arguments) {
			body, _ := args.Get(2).([]byte)
			var cmd sagaevent.Command
			_ = json.Unmarshal(body, &cmd)
			idCh <- cmd.SagaID
		})
}

func TestOrchestrator_StartAndWait_Completes(t *testing.T) {
	t.Parallel()
	store := &mockStore{}
	pub := &mockPublisher{}
	canc := &mockCanceller{}

	idCh := make(chan string, 1)
	store.On("Create", mock.Anything, mock.Anything).Return(nil)
	captureSagaID(pub, idCh)
	store.On("CompleteIfAwaiting", mock.Anything, mock.Anything).Return(true, nil)

	orch, err := NewOrchestrator(store, pub, canc, 2*time.Second, logger.Nop())
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- orch.StartAndWait(context.Background(), SubscriptionData{Email: "a@b.c", SubscriptionID: 1})
	}()

	sagaID := waitForID(t, idCh)
	require.Equal(t, messaging.Ack,
		orch.HandleReply(context.Background(), replyBody(t, sagaID, sagaevent.ReplyConfirmationSent)))

	assert.NoError(t, waitForResult(t, errCh))
	canc.AssertNotCalled(t, "Cancel", mock.Anything, mock.Anything)
}

func TestOrchestrator_StartAndWait_ConfirmationFailed_Compensates(t *testing.T) {
	t.Parallel()
	store := &mockStore{}
	pub := &mockPublisher{}
	canc := &mockCanceller{}

	idCh := make(chan string, 1)
	store.On("Create", mock.Anything, mock.Anything).Return(nil)
	captureSagaID(pub, idCh)
	store.On("ClaimCompensation", mock.Anything, mock.Anything).
		Return(SubscriptionData{SubscriptionID: 7}, true, nil)
	store.On("MarkCompensated", mock.Anything, mock.Anything).Return(nil)
	canc.On("Cancel", mock.Anything, int64(7)).Return(nil)

	orch, err := NewOrchestrator(store, pub, canc, 2*time.Second, logger.Nop())
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- orch.StartAndWait(context.Background(), SubscriptionData{SubscriptionID: 7})
	}()

	sagaID := waitForID(t, idCh)
	require.Equal(t, messaging.Ack,
		orch.HandleReply(context.Background(), replyBody(t, sagaID, sagaevent.ReplyConfirmationFailed)))

	err = waitForResult(t, errCh)
	assert.ErrorIs(t, err, ErrConfirmationFailed)
	canc.AssertCalled(t, "Cancel", mock.Anything, int64(7))
}

func TestOrchestrator_StartAndWait_Timeout(t *testing.T) {
	t.Parallel()
	store := &mockStore{}
	pub := &mockPublisher{}
	canc := &mockCanceller{}

	store.On("Create", mock.Anything, mock.Anything).Return(nil)
	pub.On("Publish", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	orch, err := NewOrchestrator(store, pub, canc, 50*time.Millisecond, logger.Nop())
	require.NoError(t, err)

	// No reply ever arrives; the call returns a timeout and leaves the saga for the reaper.
	err = orch.StartAndWait(context.Background(), SubscriptionData{SubscriptionID: 1})
	assert.ErrorIs(t, err, ErrConfirmationTimeout)
	canc.AssertNotCalled(t, "Cancel", mock.Anything, mock.Anything)
}

func TestOrchestrator_StartAndWait_PublishFailure_Compensates(t *testing.T) {
	t.Parallel()
	store := &mockStore{}
	pub := &mockPublisher{}
	canc := &mockCanceller{}

	store.On("Create", mock.Anything, mock.Anything).Return(nil)
	pub.On("Publish", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("broker down"))
	store.On("ClaimCompensation", mock.Anything, mock.Anything).
		Return(SubscriptionData{SubscriptionID: 3}, true, nil)
	store.On("MarkCompensated", mock.Anything, mock.Anything).Return(nil)
	canc.On("Cancel", mock.Anything, int64(3)).Return(nil)

	orch, err := NewOrchestrator(store, pub, canc, time.Second, logger.Nop())
	require.NoError(t, err)

	err = orch.StartAndWait(context.Background(), SubscriptionData{SubscriptionID: 3})
	assert.ErrorIs(t, err, ErrConfirmationFailed)
	canc.AssertCalled(t, "Cancel", mock.Anything, int64(3))
}

func TestOrchestrator_HandleReply_DuplicateSent_Idempotent(t *testing.T) {
	t.Parallel()
	store := &mockStore{}
	orch, err := NewOrchestrator(store, &mockPublisher{}, &mockCanceller{}, time.Second, logger.Nop())
	require.NoError(t, err)

	store.On("CompleteIfAwaiting", mock.Anything, "saga-1").Return(true, nil).Once()
	store.On("CompleteIfAwaiting", mock.Anything, "saga-1").Return(false, nil).Once()

	assert.Equal(t, messaging.Ack, orch.HandleReply(context.Background(), replyBody(t, "saga-1", sagaevent.ReplyConfirmationSent)))
	assert.Equal(t, messaging.Ack, orch.HandleReply(context.Background(), replyBody(t, "saga-1", sagaevent.ReplyConfirmationSent)))
	store.AssertExpectations(t)
}

func TestOrchestrator_HandleReply_CompensateAlreadyTerminal_NoOp(t *testing.T) {
	t.Parallel()
	store := &mockStore{}
	canc := &mockCanceller{}
	orch, err := NewOrchestrator(store, &mockPublisher{}, canc, time.Second, logger.Nop())
	require.NoError(t, err)

	// A failed reply arrives but the saga already completed: claim loses, Get
	// shows a terminal state, so no compensation runs.
	store.On("ClaimCompensation", mock.Anything, "saga-1").Return(SubscriptionData{}, false, nil)
	store.On("Get", mock.Anything, "saga-1").Return(&Instance{ID: "saga-1", State: StateCompleted}, nil)

	assert.Equal(t, messaging.Ack, orch.HandleReply(context.Background(), replyBody(t, "saga-1", sagaevent.ReplyConfirmationFailed)))
	canc.AssertNotCalled(t, "Cancel", mock.Anything, mock.Anything)
}

func TestOrchestrator_HandleReply_BadInput_Drops(t *testing.T) {
	t.Parallel()
	orch, err := NewOrchestrator(&mockStore{}, &mockPublisher{}, &mockCanceller{}, time.Second, logger.Nop())
	require.NoError(t, err)

	assert.Equal(t, messaging.Drop, orch.HandleReply(context.Background(), []byte("{bad json")))
	assert.Equal(t, messaging.Drop, orch.HandleReply(context.Background(), replyBody(t, "saga-1", "saga.unknown")))
}

func TestOrchestrator_Reap_CompensatesTimedOut(t *testing.T) {
	t.Parallel()
	store := &mockStore{}
	canc := &mockCanceller{}
	orch, err := NewOrchestrator(store, &mockPublisher{}, canc, time.Second, logger.Nop())
	require.NoError(t, err)

	store.On("ListTimedOut", mock.Anything, mock.Anything, mock.Anything).Return([]string{"saga-1"}, nil)
	store.On("ClaimCompensation", mock.Anything, "saga-1").
		Return(SubscriptionData{SubscriptionID: 9}, true, nil)
	canc.On("Cancel", mock.Anything, int64(9)).Return(nil)
	store.On("MarkCompensated", mock.Anything, "saga-1").Return(nil)

	orch.Reap(context.Background())
	canc.AssertCalled(t, "Cancel", mock.Anything, int64(9))
	store.AssertCalled(t, "MarkCompensated", mock.Anything, "saga-1")
}

func waitForID(t *testing.T, idCh <-chan string) string {
	t.Helper()
	select {
	case id := <-idCh:
		return id
	case <-time.After(time.Second):
		t.Fatal("publish was not called")
		return ""
	}
}

func waitForResult(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("StartAndWait did not return")
		return nil
	}
}
