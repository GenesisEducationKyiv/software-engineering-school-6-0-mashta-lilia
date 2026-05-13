//nolint:testpackage // mock helpers shared across service package tests
package service

import (
	"context"
	"github-release-notifier/internal/model"
)

// mockSubscriptionRepo implements SubscriptionRepo for testing.
type mockSubscriptionRepo struct {
	CreateFn           func(ctx context.Context, sub *model.Subscription) error
	GetByTokenFn       func(ctx context.Context, token string) (*model.Subscription, error)
	GetActiveByEmailFn func(ctx context.Context, email string) ([]model.Subscription, error)
	GetEmailsByRepoFn  func(ctx context.Context, owner, name string) ([]string, error)
	UpdateStatusFn     func(ctx context.Context, id int64, status model.SubscriptionStatus) error
	ExistsFn           func(ctx context.Context, email, owner, name string) (bool, error)
}

func (m *mockSubscriptionRepo) Create(ctx context.Context, sub *model.Subscription) error {
	if m.CreateFn == nil {
		panic("mockSubscriptionRepo.Create called but not configured")
	}
	return m.CreateFn(ctx, sub)
}

func (m *mockSubscriptionRepo) GetByToken(ctx context.Context, token string) (*model.Subscription, error) {
	if m.GetByTokenFn == nil {
		panic("mockSubscriptionRepo.GetByToken called but not configured")
	}
	return m.GetByTokenFn(ctx, token)
}

func (m *mockSubscriptionRepo) GetActiveByEmail(
	ctx context.Context, email string,
) ([]model.Subscription, error) {
	if m.GetActiveByEmailFn == nil {
		panic("mockSubscriptionRepo.GetActiveByEmail called but not configured")
	}
	return m.GetActiveByEmailFn(ctx, email)
}

func (m *mockSubscriptionRepo) GetEmailsByRepo(ctx context.Context, owner, name string) ([]string, error) {
	if m.GetEmailsByRepoFn == nil {
		panic("mockSubscriptionRepo.GetEmailsByRepo called but not configured")
	}
	return m.GetEmailsByRepoFn(ctx, owner, name)
}

func (m *mockSubscriptionRepo) UpdateStatus(
	ctx context.Context, id int64, status model.SubscriptionStatus,
) error {
	if m.UpdateStatusFn == nil {
		panic("mockSubscriptionRepo.UpdateStatus called but not configured")
	}
	return m.UpdateStatusFn(ctx, id, status)
}

func (m *mockSubscriptionRepo) Exists(ctx context.Context, email, owner, name string) (bool, error) {
	if m.ExistsFn == nil {
		panic("mockSubscriptionRepo.Exists called but not configured")
	}
	return m.ExistsFn(ctx, email, owner, name)
}

// mockRepoStore implements RepoStore for testing.
type mockRepoStore struct {
	UpsertFn            func(ctx context.Context, owner, name string) (*model.TrackedRepository, error)
	GetAllFn            func(ctx context.Context) ([]model.TrackedRepository, error)
	UpdateLastSeenFn    func(ctx context.Context, id int64, tag string) error
	UpdateLastCheckedFn func(ctx context.Context, id int64) error
}

func (m *mockRepoStore) Upsert(ctx context.Context, owner, name string) (*model.TrackedRepository, error) {
	if m.UpsertFn == nil {
		panic("mockRepoStore.Upsert called but not configured")
	}
	return m.UpsertFn(ctx, owner, name)
}

func (m *mockRepoStore) GetAll(ctx context.Context) ([]model.TrackedRepository, error) {
	if m.GetAllFn == nil {
		panic("mockRepoStore.GetAll called but not configured")
	}
	return m.GetAllFn(ctx)
}

func (m *mockRepoStore) UpdateLastSeen(ctx context.Context, id int64, tag string) error {
	if m.UpdateLastSeenFn == nil {
		panic("mockRepoStore.UpdateLastSeen called but not configured")
	}
	return m.UpdateLastSeenFn(ctx, id, tag)
}

func (m *mockRepoStore) UpdateLastChecked(ctx context.Context, id int64) error {
	if m.UpdateLastCheckedFn != nil {
		return m.UpdateLastCheckedFn(ctx, id)
	}
	return nil
}

// mockGitHubClient implements GitHubClient for testing.
type mockGitHubClient struct {
	RepoExistsFn       func(ctx context.Context, owner, name string) (bool, error)
	GetLatestReleaseFn func(ctx context.Context, owner, name string) (*model.Release, error)
}

func (m *mockGitHubClient) RepoExists(ctx context.Context, owner, name string) (bool, error) {
	if m.RepoExistsFn == nil {
		panic("mockGitHubClient.RepoExists called but not configured")
	}
	return m.RepoExistsFn(ctx, owner, name)
}

func (m *mockGitHubClient) GetLatestRelease(ctx context.Context, owner, name string) (*model.Release, error) {
	if m.GetLatestReleaseFn == nil {
		panic("mockGitHubClient.GetLatestRelease called but not configured")
	}
	return m.GetLatestReleaseFn(ctx, owner, name)
}

// mockMailer implements Mailer for testing.
type mockMailer struct {
	SendConfirmationFn        func(ctx context.Context, email, token, repo string) error
	SendReleaseNotificationFn func(ctx context.Context, email, repo string, release *model.Release) error
}

func (m *mockMailer) SendConfirmation(ctx context.Context, email, token, repo string) error {
	if m.SendConfirmationFn == nil {
		panic("mockMailer.SendConfirmation called but not configured")
	}
	return m.SendConfirmationFn(ctx, email, token, repo)
}

func (m *mockMailer) SendReleaseNotification(
	ctx context.Context, email, repo string, release *model.Release,
) error {
	if m.SendReleaseNotificationFn == nil {
		panic("mockMailer.SendReleaseNotification called but not configured")
	}
	return m.SendReleaseNotificationFn(ctx, email, repo, release)
}
