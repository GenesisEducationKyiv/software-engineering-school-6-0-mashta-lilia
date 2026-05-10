package service

import (
	"context"
	"github-release-notifier/internal/model"
)

// --- Subscription store roles (Interface Segregation) -----------------------
// SubscriptionService needs writer + reader. Scanner only needs the lister.
// Splitting these means Scanner's tests don't have to mock Create/Update/etc.,
// and a future read-only consumer doesn't get write power it never uses.

type SubscriptionWriter interface {
	Create(ctx context.Context, sub *model.Subscription) error
	UpdateStatus(ctx context.Context, id int64, status model.SubscriptionStatus) error
}

type SubscriptionReader interface {
	GetByToken(ctx context.Context, token string) (*model.Subscription, error)
	GetActiveByEmail(ctx context.Context, email string) ([]model.Subscription, error)
	Exists(ctx context.Context, email, owner, name string) (bool, error)
}

type SubscriberLister interface {
	GetEmailsByRepo(ctx context.Context, owner, name string) ([]string, error)
}

// SubscriptionRepo is the union — useful for the production wiring where one
// concrete *repository.SubscriptionRepo plays all three roles. Consumers
// should depend on the narrowest role they need, not on this composition.
type SubscriptionRepo interface {
	SubscriptionWriter
	SubscriptionReader
	SubscriberLister
}

// --- Tracked-repo store roles ----------------------------------------------
// SubscriptionService only upserts. Scanner reads them all and updates
// last-seen/last-checked metadata. Two separate roles.

type RepoUpserter interface {
	Upsert(ctx context.Context, owner, name string) (*model.TrackedRepository, error)
}

type RepoScanReader interface {
	GetAll(ctx context.Context) ([]model.TrackedRepository, error)
	UpdateLastSeen(ctx context.Context, id int64, tag string) error
	UpdateLastChecked(ctx context.Context, id int64) error
}

// RepoStore is the union for production wiring.
type RepoStore interface {
	RepoUpserter
	RepoScanReader
}

// --- GitHub client ---------------------------------------------------------
// Both methods are used by both consumers, so this stays unsplit.

type GitHubClient interface {
	RepoExists(ctx context.Context, owner, name string) (bool, error)
	GetLatestRelease(ctx context.Context, owner, name string) (*model.Release, error)
}

// --- Mailer roles ----------------------------------------------------------
// SubscriptionService sends confirmation emails. Scanner sends release
// notifications. Different responsibilities, different tests.

type ConfirmationSender interface {
	SendConfirmation(ctx context.Context, email, token, repo string) error
}

type ReleaseNotifier interface {
	SendReleaseNotification(ctx context.Context, email, repo string, release *model.Release) error
}

// Mailer is the union for production wiring.
type Mailer interface {
	ConfirmationSender
	ReleaseNotifier
}
