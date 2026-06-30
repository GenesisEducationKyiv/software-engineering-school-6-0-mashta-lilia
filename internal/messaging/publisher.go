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

// Publisher publishes persistent JSON messages to a durable exchange. It holds a
// single connection/channel guarded by a mutex and transparently redials after a
// failed publish, so a broker blip does not need handling at every call site.
type Publisher struct {
	url      string
	topology Topology
	log      *logger.Logger

	mu   sync.Mutex
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

// Publish sends body to the exchange under routingKey. It is safe for concurrent
// use; publishes are serialized on a single channel.
func (p *Publisher) Publish(ctx context.Context, routingKey string, body []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	ch, err := p.channel()
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
		// The channel may be dead; drop it so the next call redials.
		return errors.Join(
			fmt.Errorf("messaging: publish to %s/%s: %w", p.topology.Exchange, routingKey, err),
			p.reset(),
		)
	}
	return nil
}

// channel returns a live channel, dialing and declaring topology on first use or
// after a reset. Callers must hold p.mu.
func (p *Publisher) channel() (*amqp.Channel, error) {
	if p.ch != nil && !p.ch.IsClosed() {
		return p.ch, nil
	}
	// A dead channel can still hold an open connection; close stale state before
	// redialing so the previous connection is not leaked.
	_ = p.reset() //nolint:errcheck // best-effort close of stale state before redial
	conn, err := amqp.Dial(p.url)
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

// reset closes and clears the cached connection, returning any close error.
// Callers must hold p.mu.
func (p *Publisher) reset() error {
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
	return p.reset()
}
