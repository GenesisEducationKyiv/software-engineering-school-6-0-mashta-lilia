// Package messaging is a thin, transport-agnostic wrapper over AMQP (RabbitMQ)
// providing a reconnecting publisher and consumer. It carries opaque byte
// payloads; the message contract lives in the caller's domain package.
package messaging

import (
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	publishTimeout    = 5 * time.Second
	reconnectInterval = 2 * time.Second
	defaultPrefetch   = 16
)

// Topology is the exchange/queue layout both sides declare on connect.
// Declaration is idempotent, so the publisher can create the durable queue too
// and no command is lost while the consumer is still starting.
type Topology struct {
	Exchange    string
	Queue       string
	RoutingKeys []string
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
