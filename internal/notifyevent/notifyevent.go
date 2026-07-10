// Package notifyevent defines the wire contract shared by the monolith
// (publisher) and the notification service (consumer) for commands carried over
// the message broker. Keeping it in one place stops the two sides from drifting.
package notifyevent

import "encoding/json"

// Broker topology. A single durable direct exchange routes each command to the
// durable email queue by its type, used as the routing key.
const (
	Exchange = "notifications"
	Queue    = "notifications.email"
)

// Command types. These double as AMQP routing keys.
const (
	TypeConfirmation = "confirmation"
	TypeRelease      = "release"
)

// Envelope wraps every command so the consumer can dispatch on Type without
// guessing, and propagate the trace id across the asynchronous hop.
type Envelope struct {
	Type    string          `json:"type"`
	TraceID string          `json:"trace_id,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

// ConfirmationCommand asks the notifier to email a subscription confirmation link.
type ConfirmationCommand struct {
	Email      string `json:"email"`
	ConfirmURL string `json:"confirm_url"`
	Repo       string `json:"repo"`
}

// ReleaseCommand asks the notifier to email a new-release notification.
type ReleaseCommand struct {
	Email   string  `json:"email"`
	Repo    string  `json:"repo"`
	Release Release `json:"release"`
}

// Release is the broker-facing view of a GitHub release.
type Release struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
}

// RoutingKeys lists every routing key the email queue must be bound to.
func RoutingKeys() []string {
	return []string{TypeConfirmation, TypeRelease}
}
