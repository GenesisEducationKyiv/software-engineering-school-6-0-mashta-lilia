// Package consumer adapts broker deliveries to the notification Service. The
// decode-and-dispatch logic lives in Handle, which is pure with respect to the
// broker so it can be unit tested without RabbitMQ.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"github-release-notifier/internal/messaging"
	"github-release-notifier/internal/notifyevent"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/platform/tracectx"
	"github-release-notifier/services/notification"
)

// dispatcher is the part of *notification.Service the consumer drives.
type dispatcher interface {
	SendConfirmation(ctx context.Context, confirmation notification.Confirmation) (bool, error)
	SendReleaseNotification(
		ctx context.Context, email, repo string, rel *notification.ReleaseInfo,
	) (bool, error)
}

type Consumer struct {
	svc dispatcher
	log *logger.Logger
}

func New(svc dispatcher, log *logger.Logger) (*Consumer, error) {
	if svc == nil {
		return nil, errors.New("notification consumer: dispatcher is nil")
	}
	if log == nil {
		log = logger.Nop()
	}
	return &Consumer{svc: svc, log: log}, nil
}

// Handle decodes one command envelope and dispatches it to the service. The
// returned Action tells the broker loop how to settle the message: Drop on
// permanently bad input (so it is not redelivered forever) and Requeue on a
// transient send failure (so it is retried).
func (c *Consumer) Handle(ctx context.Context, body []byte) messaging.Action {
	var envelope notifyevent.Envelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		c.log.Error(ctx, "consume_decode_failed", "err", err)
		return messaging.Drop
	}
	if envelope.TraceID != "" {
		ctx = tracectx.WithTraceID(ctx, envelope.TraceID)
	}
	switch envelope.Type {
	case notifyevent.TypeConfirmation:
		return c.handleConfirmation(ctx, envelope.Payload)
	case notifyevent.TypeRelease:
		return c.handleRelease(ctx, envelope.Payload)
	default:
		c.log.Error(ctx, "consume_unknown_type", "type", envelope.Type)
		return messaging.Drop
	}
}

func (c *Consumer) handleConfirmation(ctx context.Context, payload []byte) messaging.Action {
	var cmd notifyevent.ConfirmationCommand
	if err := json.Unmarshal(payload, &cmd); err != nil {
		c.log.Error(ctx, "consume_decode_failed", "type", notifyevent.TypeConfirmation, "err", err)
		return messaging.Drop
	}
	if cmd.Email == "" || cmd.ConfirmURL == "" || cmd.Repo == "" {
		c.log.Error(ctx, "consume_invalid_command", "type", notifyevent.TypeConfirmation)
		return messaging.Drop
	}
	if _, err := c.svc.SendConfirmation(ctx, notification.Confirmation{
		Email:      cmd.Email,
		ConfirmURL: cmd.ConfirmURL,
		Repo:       cmd.Repo,
	}); err != nil {
		c.log.Error(ctx, "consume_send_failed", "type", notifyevent.TypeConfirmation, "err", err)
		return messaging.Requeue
	}
	return messaging.Ack
}

func (c *Consumer) handleRelease(ctx context.Context, payload []byte) messaging.Action {
	var cmd notifyevent.ReleaseCommand
	if err := json.Unmarshal(payload, &cmd); err != nil {
		c.log.Error(ctx, "consume_decode_failed", "type", notifyevent.TypeRelease, "err", err)
		return messaging.Drop
	}
	if cmd.Email == "" || cmd.Repo == "" {
		c.log.Error(ctx, "consume_invalid_command", "type", notifyevent.TypeRelease)
		return messaging.Drop
	}
	if _, err := c.svc.SendReleaseNotification(ctx, cmd.Email, cmd.Repo, &notification.ReleaseInfo{
		TagName:     cmd.Release.TagName,
		Name:        cmd.Release.Name,
		HTMLURL:     cmd.Release.HTMLURL,
		PublishedAt: cmd.Release.PublishedAt,
	}); err != nil {
		c.log.Error(ctx, "consume_send_failed", "type", notifyevent.TypeRelease, "err", err)
		return messaging.Requeue
	}
	return messaging.Ack
}
