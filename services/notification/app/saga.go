package app

import (
	"context"
	"github-release-notifier/internal/messaging"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/sagaevent"
	"github-release-notifier/services/notification/config"
)

// runSagaConsumer drives the saga participant: it consumes SendConfirmation
// commands from the orchestrator and replies with the outcome.
func runSagaConsumer(ctx context.Context, cfg *config.Config, deps *dependencies, log *logger.Logger) error {
	return messaging.RunConsumer(
		ctx,
		messaging.ConsumerConfig{
			URL: cfg.RabbitMQURL,
			Topology: messaging.Topology{
				Exchange:    sagaevent.Exchange,
				Queue:       sagaevent.CommandsQueue,
				RoutingKeys: sagaevent.CommandRoutingKeys(),
			},
			Prefetch: consumerPrefetch,
		},
		log.With("component", "saga_participant_consumer"),
		deps.sagaParticipant.Handle,
	)
}
