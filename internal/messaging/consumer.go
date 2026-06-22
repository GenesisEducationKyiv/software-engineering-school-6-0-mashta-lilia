package messaging

import (
	"context"
	"errors"
	"fmt"
	"github-release-notifier/internal/platform/logger"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Action tells the consumer loop how to settle a delivery after the handler runs.
type Action int

const (
	// Ack marks the message done and removes it from the queue.
	Ack Action = iota
	// Drop rejects the message without requeue (permanently bad input).
	Drop
	// Requeue rejects the message and returns it for redelivery (transient failure).
	Requeue
)

// Handler processes one raw message body and decides how it should be settled.
// It must not return an error: ack/nack policy is expressed through Action.
type Handler func(ctx context.Context, body []byte) Action

// ConsumerConfig configures RunConsumer.
type ConsumerConfig struct {
	URL      string
	Topology Topology
	Prefetch int
}

// RunConsumer consumes the configured queue until ctx is canceled, reconnecting
// with a fixed backoff whenever the session drops. It returns nil on clean
// shutdown (ctx canceled) and only errors on unrecoverable misconfiguration.
func RunConsumer(ctx context.Context, cfg ConsumerConfig, log *logger.Logger, handle Handler) error {
	if log == nil {
		log = logger.Nop()
	}
	if handle == nil {
		return errors.New("messaging: consumer handler is nil")
	}
	if cfg.URL == "" || cfg.Topology.Queue == "" {
		return errors.New("messaging: consumer url and queue are required")
	}
	for ctx.Err() == nil {
		if err := consumeSession(ctx, cfg, log, handle); err != nil {
			log.Error(ctx, "consumer_session_failed", "err", err)
			if !sleep(ctx, reconnectInterval) {
				return nil
			}
		}
	}
	return nil
}

// consumeSession runs one connection's worth of consumption. It returns nil when
// ctx is canceled and an error when the session drops (triggering a reconnect).
func consumeSession(ctx context.Context, cfg ConsumerConfig, log *logger.Logger, handle Handler) error {
	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return fmt.Errorf("messaging: dial broker: %w", err)
	}
	defer conn.Close() //nolint:errcheck // closing on session teardown

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("messaging: open channel: %w", err)
	}
	defer ch.Close() //nolint:errcheck // closing on session teardown

	if err := declareTopology(ch, cfg.Topology); err != nil {
		return err
	}
	prefetch := cfg.Prefetch
	if prefetch <= 0 {
		prefetch = defaultPrefetch
	}
	if err := ch.Qos(prefetch, 0, false); err != nil {
		return fmt.Errorf("messaging: set qos: %w", err)
	}

	const autoAck, exclusive, noLocal, noWait = false, false, false, false
	deliveries, err := ch.Consume(cfg.Topology.Queue, "", autoAck, exclusive, noLocal, noWait, nil)
	if err != nil {
		return fmt.Errorf("messaging: consume %s: %w", cfg.Topology.Queue, err)
	}

	closed := ch.NotifyClose(make(chan *amqp.Error, 1))
	log.Info(ctx, "consumer_started", "queue", cfg.Topology.Queue, "prefetch", prefetch)
	return pump(ctx, log, handle, deliveries, closed)
}

func pump(
	ctx context.Context,
	log *logger.Logger,
	handle Handler,
	deliveries <-chan amqp.Delivery,
	closed <-chan *amqp.Error,
) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-closed:
			if err != nil {
				return fmt.Errorf("messaging: channel closed: %w", err)
			}
			return errors.New("messaging: channel closed")
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("messaging: deliveries channel closed")
			}
			settle(ctx, log, handle, d)
		}
	}
}

func settle(ctx context.Context, log *logger.Logger, handle Handler, d amqp.Delivery) {
	const multiple, requeue = false, true
	var err error
	switch handle(ctx, d.Body) {
	case Ack:
		err = d.Ack(multiple)
	case Drop:
		err = d.Nack(multiple, !requeue)
	case Requeue:
		err = d.Nack(multiple, requeue)
	}
	if err != nil {
		log.Error(ctx, "consumer_settle_failed", "err", err)
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
