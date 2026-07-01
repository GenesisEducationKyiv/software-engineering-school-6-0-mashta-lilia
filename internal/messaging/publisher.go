package messaging

import (
	"context"
	"errors"
	"fmt"
	"github-release-notifier/internal/platform/logger"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Publisher publishes persistent JSON messages to a durable exchange, redialing transparently on failure.
type Publisher struct {
	url      string
	topology Topology
	log      *logger.Logger

	mu   sync.RWMutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

func NewPublisher(url string, topology Topology, log *logger.Logger) (*Publisher, error) {
	if url == "" {
		return nil, errors.New("messaging: broker url is empty")
	}
	if topology.Exchange == "" {
		return nil, errors.New("messaging: exchange is empty")
	}
	if log == nil {
		log = logger.Nop()
	}
	return &Publisher{url: url, topology: topology, log: log}, nil
}

// Publish sends body to the exchange under routingKey. It is safe for
// concurrent use: amqp091-go's Channel serializes its own writes internally, so
// concurrent callers only contend on the (fast) channel lookup below, not on
// the network I/O — a redial is the only path that blocks other publishers.
func (p *Publisher) Publish(ctx context.Context, routingKey string, body []byte) error {
	ch, err := p.getChannel(ctx)
	if err != nil {
		return err
	}

	pubCtx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()

	const mandatory, immediate = false, false
	err = ch.PublishWithContext(pubCtx, p.topology.Exchange, routingKey, mandatory, immediate,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
			Body:         body,
		})
	if err != nil {
		// The channel may be dead; drop it, but only if it's still the one we just
		// used, so a publish failure doesn't undo a redial another goroutine already did.
		return errors.Join(
			fmt.Errorf("messaging: publish to %s/%s: %w", p.topology.Exchange, routingKey, err),
			p.resetIfCurrent(ch),
		)
	}
	return nil
}

// getChannel returns a live channel, dialing and declaring topology if none is
// cached or the cached one has died. The common case only takes a read lock,
// so concurrent publishers don't serialize on redial bookkeeping.
func (p *Publisher) getChannel(ctx context.Context) (*amqp.Channel, error) {
	if ch, ok := p.liveChannel(); ok {
		return ch, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	// Re-check: another goroutine may have redialed while we waited for the lock.
	if p.ch != nil && !p.ch.IsClosed() {
		return p.ch, nil
	}
	_ = p.resetLocked() //nolint:errcheck // best-effort close of stale state before redial

	conn, err := dialContext(ctx, p.url)
	if err != nil {
		return nil, fmt.Errorf("messaging: dial broker: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("messaging: open channel: %w", err), conn.Close())
	}
	if err := declareTopology(ch, p.topology); err != nil {
		return nil, errors.Join(err, ch.Close(), conn.Close())
	}
	p.conn, p.ch = conn, ch
	return ch, nil
}

func (p *Publisher) liveChannel() (*amqp.Channel, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.ch != nil && !p.ch.IsClosed() {
		return p.ch, true
	}
	return nil, false
}

// resetIfCurrent clears the cached channel only if failed is still the cached
// one, so a publish failure on an already-replaced channel can't undo the
// replacement.
func (p *Publisher) resetIfCurrent(failed *amqp.Channel) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ch != failed {
		return nil
	}
	return p.resetLocked()
}

// resetLocked closes and clears the cached connection. Callers must hold p.mu (write lock).
func (p *Publisher) resetLocked() error {
	var err error
	if p.ch != nil {
		err = errors.Join(err, p.ch.Close())
		p.ch = nil
	}
	if p.conn != nil {
		err = errors.Join(err, p.conn.Close())
		p.conn = nil
	}
	return err
}

// Close releases the connection. Safe to call once at shutdown.
func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resetLocked()
}
