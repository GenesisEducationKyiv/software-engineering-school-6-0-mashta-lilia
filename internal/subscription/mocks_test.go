//nolint:testpackage // white-box tests use unexported interfaces
package subscription

import (
	"context"
	"github-release-notifier/internal/saga"

	"github.com/stretchr/testify/mock"
)

type mockSubscriptionRepo struct {
	mock.Mock
}

func (m *mockSubscriptionRepo) Create(ctx context.Context, sub *Subscription) error {
	return m.Called(ctx, sub).Error(0)
}

func (m *mockSubscriptionRepo) GetByToken(ctx context.Context, token string) (*Subscription, error) {
	args := m.Called(ctx, token)
	sub, _ := args.Get(0).(*Subscription)
	return sub, args.Error(1)
}

func (m *mockSubscriptionRepo) GetActiveByEmail(ctx context.Context, email string) ([]Subscription, error) {
	args := m.Called(ctx, email)
	subs, _ := args.Get(0).([]Subscription)
	return subs, args.Error(1)
}

func (m *mockSubscriptionRepo) GetByEmailAndRepo(
	ctx context.Context, email, owner, name string,
) (*Subscription, error) {
	args := m.Called(ctx, email, owner, name)
	sub, _ := args.Get(0).(*Subscription)
	return sub, args.Error(1)
}

func (m *mockSubscriptionRepo) UpdateStatus(ctx context.Context, id int64, status Status) error {
	return m.Called(ctx, id, status).Error(0)
}

func (m *mockSubscriptionRepo) UpdateToken(ctx context.Context, id int64, oldToken, newToken string) error {
	return m.Called(ctx, id, oldToken, newToken).Error(0)
}

type mockRepoUpserter struct {
	mock.Mock
}

func (m *mockRepoUpserter) Upsert(ctx context.Context, owner, name string) error {
	return m.Called(ctx, owner, name).Error(0)
}

type mockGitHubChecker struct {
	mock.Mock
}

func (m *mockGitHubChecker) RepoExists(ctx context.Context, owner, name string) (bool, error) {
	args := m.Called(ctx, owner, name)
	return args.Bool(0), args.Error(1)
}

type mockSubscriptionSaga struct {
	mock.Mock
}

func (m *mockSubscriptionSaga) StartAndWait(ctx context.Context, data saga.SubscriptionData) error {
	return m.Called(ctx, data).Error(0)
}

type mockTokenGenerator struct {
	mock.Mock
}

func (m *mockTokenGenerator) Generate() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}
