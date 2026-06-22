package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github-release-notifier/internal/notifyevent"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/platform/tracectx"
	"github-release-notifier/internal/release"
)

// broker is the slice of messaging.Publisher this package needs; an interface so
// it can be faked in tests.
type broker interface {
	Publish(ctx context.Context, routingKey string, body []byte) error
}

// Publisher turns notification requests into broker commands. It satisfies the
// monolith's confirmationSender and releaseNotifier ports, so the subscription
// service and poller publish asynchronously instead of calling the notifier
// over gRPC.
type Publisher struct {
	broker broker
	log    *logger.Logger
}

func NewPublisher(b broker, log *logger.Logger) (*Publisher, error) {
	if b == nil {
		return nil, errors.New("notification publisher: broker is nil")
	}
	if log == nil {
		log = logger.Nop()
	}
	return &Publisher{broker: b, log: log}, nil
}

func (p *Publisher) SendConfirmation(ctx context.Context, email, confirmURL, repo string) error {
	payload, err := json.Marshal(notifyevent.ConfirmationCommand{
		Email:      email,
		ConfirmURL: confirmURL,
		Repo:       repo,
	})
	if err != nil {
		return fmt.Errorf("marshaling confirmation command: %w", err)
	}
	return p.publish(ctx, notifyevent.TypeConfirmation, payload)
}

func (p *Publisher) SendReleaseNotification(
	ctx context.Context, email, repo string, rel *release.Release,
) error {
	payload, err := json.Marshal(notifyevent.ReleaseCommand{
		Email:   email,
		Repo:    repo,
		Release: releaseToEvent(rel),
	})
	if err != nil {
		return fmt.Errorf("marshaling release command: %w", err)
	}
	return p.publish(ctx, notifyevent.TypeRelease, payload)
}

func (p *Publisher) publish(ctx context.Context, msgType string, payload json.RawMessage) error {
	envelope := notifyevent.Envelope{Type: msgType, Payload: payload}
	if traceID, ok := tracectx.FromContext(ctx); ok {
		envelope.TraceID = traceID
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshaling %s envelope: %w", msgType, err)
	}
	if err := p.broker.Publish(ctx, msgType, body); err != nil {
		return fmt.Errorf("publishing %s command: %w", msgType, err)
	}
	return nil
}

func releaseToEvent(rel *release.Release) notifyevent.Release {
	if rel == nil {
		return notifyevent.Release{}
	}
	return notifyevent.Release{
		TagName:     rel.TagName,
		Name:        rel.Name,
		HTMLURL:     rel.HTMLURL,
		PublishedAt: rel.PublishedAt,
	}
}
