package subscription

import (
	"context"
)

// subscriptionStore groups write-side mutation (Create, UpdateStatus, Exists,
// used by Subscribe/Confirm/Unsubscribe) and token-keyed reads (GetByToken,
// GetActiveByEmail). Kept as one interface because the package as a whole is
// the consumer (ADR-0009); split if a sixth method appears.
type subscriptionStore interface {
	// Write side.
	Create(ctx context.Context, sub *Subscription) error
	UpdateStatus(ctx context.Context, id int64, status Status) error
	Exists(ctx context.Context, email, owner, name string) (bool, error)

	// Read side.
	GetByToken(ctx context.Context, token string) (*Subscription, error)
	GetActiveByEmail(ctx context.Context, email string) ([]Subscription, error)
}

type repoUpserter interface {
	Upsert(ctx context.Context, owner, name string) error
}

type githubChecker interface {
	RepoExists(ctx context.Context, owner, name string) (bool, error)
}

type confirmationSender interface {
	SendConfirmation(ctx context.Context, email, token, repo string) error
}

type tokenGen interface {
	Generate() (string, error)
}
