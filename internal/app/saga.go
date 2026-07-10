package app

import (
	"context"
	"github-release-notifier/internal/config"
	"github-release-notifier/internal/messaging"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/sagaevent"
	"time"
)

// sagaReaperInterval is how often the orchestrator scans for timed-out sagas;
// kept below the saga timeout so a stranded saga is compensated promptly.
const sagaReaperInterval = 15 * time.Second

// runSagaReplyConsumer drives the orchestrator's reply consumer until ctx is
// canceled, reconnecting on broker blips.
func runSagaReplyConsumer(ctx context.Context, cfg *config.Config, deps *dependencies, log *logger.Logger) {
	err := messaging.RunConsumer(
		ctx,
		messaging.ConsumerConfig{
			URL: cfg.RabbitMQURL,
			Topology: messaging.Topology{
				Exchange:    sagaevent.Exchange,
				Queue:       sagaevent.RepliesQueue,
				RoutingKeys: sagaevent.ReplyRoutingKeys(),
			},
		},
		log.With("component", "saga_reply_consumer"),
		deps.orchestrator.HandleReply,
	)
	if err != nil {
		log.Error(ctx, "saga_reply_consumer_failed", "err", err)
	}
}
