package subscription

import (
	"context"
)

// Kept as one interface because the package itself is the consumer (ADR-0009).
type subscriptionStore interface {
	Create(ctx context.Context, sub *Subscription) error
	UpdateStatus(ctx context.Context, id int64, status Status) error
	Exists(ctx context.Context, email, owner, name string) (bool, error)
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
