// Package saga implements an orchestrated saga for the subscribe distributed
// transaction: a local "create pending subscription" step in the monolith plus
// a remote "send confirmation" step in the notification service, coordinated
// with compensation if the remote step fails or times out.
package saga

import "time"

// State is the saga's position in its lifecycle.
type State string

const (
	// StateAwaitingConfirmation: subscription reserved, confirmation command sent.
	StateAwaitingConfirmation State = "awaiting_confirmation"
	// StateCompleted: confirmation delivered; the transaction succeeded.
	StateCompleted State = "completed"
	// StateCompensating: confirmation failed/timed out; canceling the subscription.
	StateCompensating State = "compensating"
	// StateFailed: terminal after compensation (or an unrecoverable start error).
	StateFailed State = "failed"
)

// TypeSubscription is the only saga type today; kept explicit for the stored row.
const TypeSubscription = "subscription"

// SubscriptionData is the saga's working set, persisted as JSON so a recovered
// orchestrator can resume or compensate without re-deriving anything.
type SubscriptionData struct {
	Email          string `json:"email"`
	Repo           string `json:"repo"`
	Owner          string `json:"owner"`
	Name           string `json:"name"`
	Token          string `json:"token"`
	ConfirmURL     string `json:"confirm_url"`
	SubscriptionID int64  `json:"subscription_id"`
}

// Instance is one persisted saga.
type Instance struct {
	ID        string
	Type      string
	State     State
	Data      SubscriptionData
	TimeoutAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}
