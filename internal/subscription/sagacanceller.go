package subscription

import (
	"context"
	"errors"
)

// statusUpdater is the slice of the store the canceller needs.
type statusUpdater interface {
	UpdateStatus(ctx context.Context, id int64, status Status) error
}

// SagaCanceller is the saga's compensation port (mark unsubscribed), built from
// the repo not the Service so the orchestrator and Service have no construction cycle.
type SagaCanceller struct {
	subs statusUpdater
}

func NewSagaCanceller(subs statusUpdater) *SagaCanceller {
	return &SagaCanceller{subs: subs}
}

// Cancel marks the subscription unsubscribed; a missing row is treated as success
// so compensation is idempotent and retry-safe.
func (c *SagaCanceller) Cancel(ctx context.Context, subscriptionID int64) error {
	if err := c.subs.UpdateStatus(ctx, subscriptionID, StatusUnsubscribed); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	return nil
}
