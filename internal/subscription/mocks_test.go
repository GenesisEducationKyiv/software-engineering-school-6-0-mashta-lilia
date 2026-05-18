//nolint:testpackage // white-box tests use unexported interfaces
package subscription

import (
	"context"
)

type mockSubscriptionRepo struct {
	CreateFn           func(ctx context.Context, sub *Subscription) error
	GetByTokenFn       func(ctx context.Context, token string) (*Subscription, error)
	GetActiveByEmailFn func(ctx context.Context, email string) ([]Subscription, error)
	UpdateStatusFn     func(ctx context.Context, id int64, status Status) error
	ExistsFn           func(ctx context.Context, email, owner, name string) (bool, error)
}

func (m *mockSubscriptionRepo) Create(ctx context.Context, sub *Subscription) error {
	if m.CreateFn == nil {
		panic("mockSubscriptionRepo.Create called but not configured")
	}
	return m.CreateFn(ctx, sub)
}

func (m *mockSubscriptionRepo) GetByToken(ctx context.Context, token string) (*Subscription, error) {
	if m.GetByTokenFn == nil {
		panic("mockSubscriptionRepo.GetByToken called but not configured")
	}
	return m.GetByTokenFn(ctx, token)
}

func (m *mockSubscriptionRepo) GetActiveByEmail(ctx context.Context, email string) ([]Subscription, error) {
	if m.GetActiveByEmailFn == nil {
		panic("mockSubscriptionRepo.GetActiveByEmail called but not configured")
	}
	return m.GetActiveByEmailFn(ctx, email)
}

func (m *mockSubscriptionRepo) UpdateStatus(ctx context.Context, id int64, status Status) error {
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

type mockRepoUpserter struct {
	UpsertFn func(ctx context.Context, owner, name string) error
}

func (m *mockRepoUpserter) Upsert(ctx context.Context, owner, name string) error {
	if m.UpsertFn == nil {
		panic("mockRepoUpserter.Upsert called but not configured")
	}
	return m.UpsertFn(ctx, owner, name)
}

type mockGitHubChecker struct {
	RepoExistsFn func(ctx context.Context, owner, name string) (bool, error)
}

func (m *mockGitHubChecker) RepoExists(ctx context.Context, owner, name string) (bool, error) {
	if m.RepoExistsFn == nil {
		panic("mockGitHubChecker.RepoExists called but not configured")
	}
	return m.RepoExistsFn(ctx, owner, name)
}

type mockConfirmationSender struct {
	SendConfirmationFn func(ctx context.Context, email, token, repo string) error
}

func (m *mockConfirmationSender) SendConfirmation(ctx context.Context, email, token, repo string) error {
	if m.SendConfirmationFn == nil {
		panic("mockConfirmationSender.SendConfirmation called but not configured")
	}
	return m.SendConfirmationFn(ctx, email, token, repo)
}

// fixedTokenGenerator is a deterministic generator for tests.
type fixedTokenGenerator struct {
	Token string
	Err   error
}

func (g fixedTokenGenerator) Generate() (string, error) {
	return g.Token, g.Err
}
