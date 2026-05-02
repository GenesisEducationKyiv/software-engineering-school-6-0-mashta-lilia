package service

import (
	"context"
	"github-release-notifier/internal/model"
)

//nolint:dupl // SubscriptionRepo interface is intentionally similar to its mock implementation
type SubscriptionRepo interface {
	Create(ctx context.Context, sub *model.Subscription) error
	GetByToken(ctx context.Context, token string) (*model.Subscription, error)
	GetActiveByEmail(ctx context.Context, email string) ([]model.Subscription, error)
	GetEmailsByRepo(ctx context.Context, owner, name string) ([]string, error)
	UpdateStatus(ctx context.Context, id int64, status model.SubscriptionStatus) error
	Exists(ctx context.Context, email, owner, name string) (bool, error)
}

type RepoStore interface {
	Upsert(ctx context.Context, owner, name string) (*model.TrackedRepository, error)
	GetAll(ctx context.Context) ([]model.TrackedRepository, error)
	UpdateLastSeen(ctx context.Context, id int64, tag string) error
	UpdateLastChecked(ctx context.Context, id int64) error
}

type GitHubClient interface {
	RepoExists(ctx context.Context, owner, name string) (bool, error)
	GetLatestRelease(ctx context.Context, owner, name string) (*model.Release, error)
}

type Mailer interface {
	SendConfirmation(ctx context.Context, email, token, repo string) error
	SendReleaseNotification(ctx context.Context, email, repo string, release *model.Release) error
}
