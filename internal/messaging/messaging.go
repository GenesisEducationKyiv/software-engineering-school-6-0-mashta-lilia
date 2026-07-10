// Package messaging is a thin, transport-agnostic wrapper over AMQP (RabbitMQ)
// providing a reconnecting publisher and consumer. It carries opaque byte
// payloads; the message contract lives in the caller's domain package.
package messaging

import (
	"context"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	publishTimeout    = 5 * time.Second
	reconnectInterval = 2 * time.Second
	defaultPrefetch   = 16
)

// Topology is the exchange/queue layout both sides idempotently declare on connect.
type Topology struct {
	Exchange    string
	Queue       string
	RoutingKeys []string
}

// dialContext dials the broker, aborting early if ctx is canceled first; plain
// amqp.Dial takes no context and would otherwise block shutdown indefinitely on
// a stalled TCP/AMQP handshake.
func dialContext(ctx context.Context, url string) (*amqp.Connection, error) {
	type dialResult struct {
		conn *amqp.Connection
		err  error
	}
	done := make(chan dialResult, 1)
	go func() {
		conn, err := amqp.Dial(url)
		done <- dialResult{conn, err}
	}()
	select {
	case <-ctx.Done():
		// Close the connection in the background if the dial succeeds after we've
		// already given up, so it isn't leaked.
		go func() {
			if r := <-done; r.conn != nil {
				_ = r.conn.Close() //nolint:errcheck // best-effort close of a discarded connection
			}
		}()
		return nil, ctx.Err()
	case r := <-done:
		return r.conn, r.err
	}
}

func declareTopology(ch *amqp.Channel, topology Topology) error {
	const (
		durable    = true
		autoDelete = false
		internal   = false
		noWait     = false
		exclusive  = false
	)
	err := ch.ExchangeDeclare(
		topology.Exchange, amqp.ExchangeDirect, durable, autoDelete, internal, noWait, nil,
	)
	if err != nil {
		return fmt.Errorf("messaging: declare exchange %s: %w", topology.Exchange, err)
	}
	if topology.Queue == "" {
		return nil
	}
	if _, err := ch.QueueDeclare(
		topology.Queue, durable, autoDelete, exclusive, noWait, nil,
	); err != nil {
		return fmt.Errorf("messaging: declare queue %s: %w", topology.Queue, err)
	}
	for _, key := range topology.RoutingKeys {
		if err := ch.QueueBind(topology.Queue, key, topology.Exchange, noWait, nil); err != nil {
			return fmt.Errorf("messaging: bind queue %s to %s: %w", topology.Queue, key, err)
		}
	}
	return nil
}
