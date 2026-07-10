package subscription

import (
	"context"
	"github-release-notifier/internal/saga"
)

// Kept as one interface because the package itself is the consumer (ADR-0009).
type subscriptionStore interface {
	Create(ctx context.Context, sub *Subscription) error
	UpdateStatus(ctx context.Context, id int64, status Status) error
	UpdateToken(ctx context.Context, id int64, oldToken, newToken string) error
	GetByEmailAndRepo(ctx context.Context, email, owner, name string) (*Subscription, error)
	GetByToken(ctx context.Context, token string) (*Subscription, error)
	GetActiveByEmail(ctx context.Context, email string) ([]Subscription, error)
}

type repoUpserter interface {
	Upsert(ctx context.Context, owner, name string) error
}

type githubChecker interface {
	RepoExists(ctx context.Context, owner, name string) (bool, error)
}

// subscriptionSaga runs the orchestrated confirmation saga and blocks for the outcome.
type subscriptionSaga interface {
	StartAndWait(ctx context.Context, data saga.SubscriptionData) error
}

// confirmationLinkBuilder lives in the monolith so the notifier never has to know the
// public base URL or the /api/confirm/{token} route — that stays a monolith concern.
type confirmationLinkBuilder interface {
	ConfirmURL(token string) string
}

type tokenGen interface {
	Generate() (string, error)
}
