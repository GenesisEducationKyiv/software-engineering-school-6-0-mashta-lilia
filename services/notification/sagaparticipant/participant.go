// Package sagaparticipant consumes SendConfirmation commands and replies
// sent/failed to the orchestrator; reprocessing is safe via the Service's
// confirm-URL dedup.
package sagaparticipant

import (
	"context"
	"encoding/json"
	"errors"
	"github-release-notifier/internal/messaging"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/platform/tracectx"
	"github-release-notifier/internal/sagaevent"
	"github-release-notifier/services/notification"
)

type confirmationSender interface {
	SendConfirmation(ctx context.Context, confirmation notification.Confirmation) (bool, error)
}

type replyPublisher interface {
	Publish(ctx context.Context, routingKey string, body []byte) error
}

type Participant struct {
	sender    confirmationSender
	publisher replyPublisher
	log       *logger.Logger
}

func New(sender confirmationSender, publisher replyPublisher, log *logger.Logger) (*Participant, error) {
	if sender == nil {
		return nil, errors.New("saga participant: sender is nil")
	}
	if publisher == nil {
		return nil, errors.New("saga participant: reply publisher is nil")
	}
	if log == nil {
		log = logger.Nop()
	}
	return &Participant{sender: sender, publisher: publisher, log: log}, nil
}

// Handle processes one SendConfirmation command and replies to the orchestrator.
func (p *Participant) Handle(ctx context.Context, body []byte) messaging.Action {
	var cmd sagaevent.Command
	if err := json.Unmarshal(body, &cmd); err != nil {
		p.log.Error(ctx, "saga_command_decode_failed", "err", err)
		return messaging.Drop
	}
	if cmd.TraceID != "" {
		ctx = tracectx.WithTraceID(ctx, cmd.TraceID)
	}
	if cmd.Type != sagaevent.CmdSendConfirmation {
		p.log.Error(ctx, "saga_command_unknown_type", "type", cmd.Type)
		return messaging.Drop
	}

	var payload sagaevent.SendConfirmationPayload
	if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
		p.log.Error(ctx, "saga_command_decode_failed", "type", cmd.Type, "err", err)
		return p.reply(ctx, cmd, sagaevent.ReplyConfirmationFailed, "invalid payload")
	}
	if payload.Email == "" || payload.ConfirmURL == "" || payload.Repo == "" {
		p.log.Error(ctx, "saga_command_invalid", "saga_id", cmd.SagaID)
		return p.reply(ctx, cmd, sagaevent.ReplyConfirmationFailed, "missing fields")
	}

	if _, err := p.sender.SendConfirmation(ctx, notification.Confirmation{
		Email:      payload.Email,
		ConfirmURL: payload.ConfirmURL,
		Repo:       payload.Repo,
	}); err != nil {
		p.log.Error(ctx, "saga_confirmation_send_failed", "saga_id", cmd.SagaID, "err", err)
		return p.reply(ctx, cmd, sagaevent.ReplyConfirmationFailed, err.Error())
	}
	return p.reply(ctx, cmd, sagaevent.ReplyConfirmationSent, "")
}

// reply publishes the outcome; a publish failure requeues the command so it is
// reprocessed (the dedup ledger makes the resend a no-op) and the reply re-sent.
func (p *Participant) reply(
	ctx context.Context, cmd sagaevent.Command, replyType, reason string,
) messaging.Action {
	reply := sagaevent.Reply{SagaID: cmd.SagaID, Type: replyType, TraceID: cmd.TraceID}
	if reason != "" {
		if payload, err := json.Marshal(sagaevent.ConfirmationFailedPayload{Reason: reason}); err == nil {
			reply.Payload = payload
		}
	}
	body, err := json.Marshal(reply)
	if err != nil {
		p.log.Error(ctx, "saga_reply_marshal_failed", "saga_id", cmd.SagaID, "err", err)
		return messaging.Requeue
	}
	if err := p.publisher.Publish(ctx, replyType, body); err != nil {
		p.log.Error(ctx, "saga_reply_publish_failed", "saga_id", cmd.SagaID, "err", err)
		return messaging.Requeue
	}
	return messaging.Ack
}
