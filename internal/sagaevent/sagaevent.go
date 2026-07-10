// Package sagaevent is the wire contract shared by the saga orchestrator
// (monolith) and the notification participant (notifier): the command/reply
// envelopes carried over the broker. Keeping it in one package stops the two
// sides from drifting.
package sagaevent

import "encoding/json"

// Broker topology. A single durable direct exchange routes commands to the
// participant queue and replies back to the orchestrator queue by type.
const (
	Exchange      = "saga"
	CommandsQueue = "saga.commands"
	RepliesQueue  = "saga.replies"
)

// Command types (orchestrator -> participant). These double as routing keys.
const (
	CmdSendConfirmation = "saga.send_confirmation"
)

// Reply types (participant -> orchestrator). These double as routing keys.
const (
	ReplyConfirmationSent   = "saga.confirmation_sent"
	ReplyConfirmationFailed = "saga.confirmation_failed"
)

// Command is the orchestrator's instruction to a participant.
type Command struct {
	SagaID  string          `json:"saga_id"`
	Type    string          `json:"type"`
	TraceID string          `json:"trace_id,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

// Reply is a participant's response, correlated back to the saga by SagaID.
type Reply struct {
	SagaID  string          `json:"saga_id"`
	Type    string          `json:"type"`
	TraceID string          `json:"trace_id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// SendConfirmationPayload is everything the notifier needs to send a confirmation.
type SendConfirmationPayload struct {
	Email      string `json:"email"`
	Repo       string `json:"repo"`
	ConfirmURL string `json:"confirm_url"`
}

// ConfirmationFailedPayload carries the failure reason for the orchestrator's logs.
type ConfirmationFailedPayload struct {
	Reason string `json:"reason"`
}

// CommandRoutingKeys lists the keys the participant command queue is bound to.
func CommandRoutingKeys() []string { return []string{CmdSendConfirmation} }

// ReplyRoutingKeys lists the keys the orchestrator reply queue is bound to.
func ReplyRoutingKeys() []string {
	return []string{ReplyConfirmationSent, ReplyConfirmationFailed}
}
