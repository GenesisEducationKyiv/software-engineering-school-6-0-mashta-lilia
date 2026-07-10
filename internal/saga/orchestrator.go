package saga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github-release-notifier/internal/messaging"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/platform/tracectx"
	"github-release-notifier/internal/sagaevent"
	"sync"
	"time"
)

const (
	defaultTimeout = 30 * time.Second
	reaperBatch    = 100
)

var (
	// ErrConfirmationFailed is returned when the notifier reported a failure (or
	// the command could not be dispatched); the subscription was compensated.
	ErrConfirmationFailed = errors.New("saga: confirmation failed")
	// ErrConfirmationTimeout is returned when no reply arrived in time; the
	// reaper will compensate the subscription.
	ErrConfirmationTimeout = errors.New("saga: confirmation timed out")
)

// sagaStore persists saga state with single-winner transition guards.
type sagaStore interface {
	Create(ctx context.Context, in Instance) error
	Get(ctx context.Context, id string) (*Instance, error)
	CompleteIfAwaiting(ctx context.Context, id string) (bool, error)
	ClaimCompensation(ctx context.Context, id string) (SubscriptionData, bool, error)
	MarkCompensated(ctx context.Context, id string) error
	ListTimedOut(ctx context.Context, now time.Time, limit int) ([]string, error)
}

// commandPublisher publishes saga commands to the broker.
type commandPublisher interface {
	Publish(ctx context.Context, routingKey string, body []byte) error
}

// subscriptionCanceller is the compensation port: undo the local pending row.
type subscriptionCanceller interface {
	Cancel(ctx context.Context, subscriptionID int64) error
}

// Orchestrator coordinates the subscribe saga: confirmation dispatch, blocking
// for the outcome, and compensation on failure or timeout.
type Orchestrator struct {
	store     sagaStore
	publisher commandPublisher
	canceller subscriptionCanceller
	timeout   time.Duration
	log       *logger.Logger

	mu      sync.Mutex
	waiters map[string]chan State
}

func NewOrchestrator(
	store sagaStore,
	publisher commandPublisher,
	canceller subscriptionCanceller,
	timeout time.Duration,
	log *logger.Logger,
) (*Orchestrator, error) {
	if store == nil || publisher == nil || canceller == nil {
		return nil, errors.New("saga orchestrator: nil dependency")
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if log == nil {
		log = logger.Nop()
	}
	return &Orchestrator{
		store:     store,
		publisher: publisher,
		canceller: canceller,
		timeout:   timeout,
		log:       log,
		waiters:   make(map[string]chan State),
	}, nil
}

// StartAndWait blocks until the saga completes, is compensated, or times out;
// the caller has reserved the pending row and the saga owns confirm + compensation.
func (o *Orchestrator) StartAndWait(ctx context.Context, data SubscriptionData) error {
	sagaID := tracectx.NewID()
	inst := Instance{
		ID:        sagaID,
		Type:      TypeSubscription,
		State:     StateAwaitingConfirmation,
		Data:      data,
		TimeoutAt: time.Now().Add(o.timeout),
	}
	if err := o.store.Create(ctx, inst); err != nil {
		o.cancelQuietly(ctx, data.SubscriptionID)
		return fmt.Errorf("saga: start: %w", err)
	}

	ch := o.registerWaiter(sagaID)
	defer o.unregisterWaiter(sagaID)

	if err := o.publishConfirmation(ctx, sagaID, data); err != nil {
		o.compensateQuietly(ctx, sagaID)
		return fmt.Errorf("%w: %w", ErrConfirmationFailed, err)
	}

	select {
	case final := <-ch:
		if final == StateCompleted {
			return nil
		}
		return ErrConfirmationFailed
	case <-time.After(o.timeout):
		return ErrConfirmationTimeout // the reaper compensates the stranded saga
	case <-ctx.Done():
		return ctx.Err()
	}
}

// HandleReply is the broker handler for participant replies.
func (o *Orchestrator) HandleReply(ctx context.Context, body []byte) messaging.Action {
	var reply sagaevent.Reply
	if err := json.Unmarshal(body, &reply); err != nil {
		o.log.Error(ctx, "saga_reply_decode_failed", "err", err)
		return messaging.Drop
	}
	if reply.TraceID != "" {
		ctx = tracectx.WithTraceID(ctx, reply.TraceID)
	}
	switch reply.Type {
	case sagaevent.ReplyConfirmationSent:
		return o.onConfirmationSent(ctx, reply.SagaID)
	case sagaevent.ReplyConfirmationFailed:
		return o.onConfirmationFailed(ctx, reply.SagaID)
	default:
		o.log.Error(ctx, "saga_reply_unknown_type", "type", reply.Type)
		return messaging.Drop
	}
}

func (o *Orchestrator) onConfirmationSent(ctx context.Context, sagaID string) messaging.Action {
	transitioned, err := o.store.CompleteIfAwaiting(ctx, sagaID)
	if err != nil {
		o.log.Error(ctx, "saga_complete_failed", "saga_id", sagaID, "err", err)
		return messaging.Requeue
	}
	if transitioned {
		o.log.Info(ctx, "saga_completed", "saga_id", sagaID)
		o.signalWaiter(sagaID, StateCompleted)
	}
	return messaging.Ack
}

func (o *Orchestrator) onConfirmationFailed(ctx context.Context, sagaID string) messaging.Action {
	if err := o.compensate(ctx, sagaID); err != nil {
		o.log.Error(ctx, "saga_compensate_failed", "saga_id", sagaID, "err", err)
		return messaging.Requeue
	}
	o.signalWaiter(sagaID, StateFailed)
	return messaging.Ack
}

// Reap compensates sagas whose confirmation reply never arrived.
func (o *Orchestrator) Reap(ctx context.Context) {
	ids, err := o.store.ListTimedOut(ctx, time.Now(), reaperBatch)
	if err != nil {
		o.log.Error(ctx, "saga_reaper_list_failed", "err", err)
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		o.log.Warn(ctx, "saga_timed_out", "saga_id", id)
		if err := o.compensate(ctx, id); err != nil {
			o.log.Error(ctx, "saga_reaper_compensate_failed", "saga_id", id, "err", err)
		}
	}
}

// RunReaper scans for timed-out sagas on an interval until ctx is canceled.
func (o *Orchestrator) RunReaper(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = o.timeout
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.Reap(ctx)
		}
	}
}

// compensate cancels the subscription once: the claim guards the
// awaiting->compensating transition, and a saga stuck in compensating is resumed.
func (o *Orchestrator) compensate(ctx context.Context, sagaID string) error {
	data, claimed, err := o.store.ClaimCompensation(ctx, sagaID)
	if err != nil {
		return err
	}
	if !claimed {
		inst, gerr := o.store.Get(ctx, sagaID)
		if gerr != nil {
			return gerr
		}
		if inst.State != StateCompensating {
			return nil // already completed or failed — nothing to undo
		}
		data = inst.Data
	}
	if err := o.canceller.Cancel(ctx, data.SubscriptionID); err != nil {
		return fmt.Errorf("saga: cancel subscription %d: %w", data.SubscriptionID, err)
	}
	if err := o.store.MarkCompensated(ctx, sagaID); err != nil {
		return err
	}
	o.log.Info(ctx, "saga_compensated", "saga_id", sagaID, "subscription_id", data.SubscriptionID)
	return nil
}

func (o *Orchestrator) publishConfirmation(ctx context.Context, sagaID string, data SubscriptionData) error {
	payload, err := json.Marshal(sagaevent.SendConfirmationPayload{
		Email:      data.Email,
		Repo:       data.Repo,
		ConfirmURL: data.ConfirmURL,
	})
	if err != nil {
		return fmt.Errorf("marshal confirmation payload: %w", err)
	}
	cmd := sagaevent.Command{SagaID: sagaID, Type: sagaevent.CmdSendConfirmation, Payload: payload}
	if traceID, ok := tracectx.FromContext(ctx); ok {
		cmd.TraceID = traceID
	}
	body, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal confirmation command: %w", err)
	}
	return o.publisher.Publish(ctx, sagaevent.CmdSendConfirmation, body)
}

// cancelQuietly rolls back the reserved subscription when the saga cannot be
// persisted, detached so it survives request cancellation.
func (o *Orchestrator) cancelQuietly(ctx context.Context, subscriptionID int64) {
	if err := o.canceller.Cancel(context.WithoutCancel(ctx), subscriptionID); err != nil {
		o.log.Error(ctx, "saga_start_rollback_failed", "subscription_id", subscriptionID, "err", err)
	}
}

func (o *Orchestrator) compensateQuietly(ctx context.Context, sagaID string) {
	if err := o.compensate(context.WithoutCancel(ctx), sagaID); err != nil {
		o.log.Error(ctx, "saga_compensate_failed", "saga_id", sagaID, "err", err)
	}
}

func (o *Orchestrator) registerWaiter(sagaID string) <-chan State {
	ch := make(chan State, 1)
	o.mu.Lock()
	o.waiters[sagaID] = ch
	o.mu.Unlock()
	return ch
}

func (o *Orchestrator) unregisterWaiter(sagaID string) {
	o.mu.Lock()
	delete(o.waiters, sagaID)
	o.mu.Unlock()
}

func (o *Orchestrator) signalWaiter(sagaID string, state State) {
	o.mu.Lock()
	ch, ok := o.waiters[sagaID]
	o.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- state:
	default: // buffered cap 1; a late second signal is dropped
	}
}
