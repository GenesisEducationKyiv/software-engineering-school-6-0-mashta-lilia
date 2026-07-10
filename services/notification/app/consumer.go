package app

import (
	"context"
	"github-release-notifier/internal/messaging"
	"github-release-notifier/internal/notifyevent"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/services/notification/config"
)

const consumerPrefetch = 16

// runConsumer drives the broker consumer until ctx is canceled, reconnecting on
// its own when the broker drops.
func runConsumer(ctx context.Context, cfg *config.Config, deps *dependencies, log *logger.Logger) error {
	return messaging.RunConsumer(
		ctx,
		messaging.ConsumerConfig{
			URL: cfg.RabbitMQURL,
			Topology: messaging.Topology{
				Exchange:    notifyevent.Exchange,
				Queue:       notifyevent.Queue,
				RoutingKeys: notifyevent.RoutingKeys(),
			},
			Prefetch: consumerPrefetch,
		},
		log.With("component", "notification_consumer"),
		deps.consumer.Handle,
	)
}
