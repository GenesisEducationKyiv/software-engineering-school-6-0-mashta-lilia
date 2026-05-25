//nolint:testpackage // white-box tests share unexported helpers
package subscription

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testEmail = "user@example.com"
	testToken = "test-token-deterministic"
)

func newTestService(
	subs *mockSubscriptionRepo,
	repos *mockRepoUpserter,
	gh *mockGitHubChecker,
	mail *mockConfirmationSender,
) *Service {
	return NewService(subs, repos, gh, mail, fixedTokenGenerator{Token: testToken})
}

func TestNewService_PanicsOnNilDependency(t *testing.T) {
	t.Parallel()
	subs := &mockSubscriptionRepo{}
	repos := &mockRepoUpserter{}
	gh := &mockGitHubChecker{}
	mail := &mockConfirmationSender{}
	tok := fixedTokenGenerator{Token: testToken}

	cases := []struct {
		name string
		args [5]any
	}{
		{"subs", [5]any{nil, repos, gh, mail, tok}},
		{"repos", [5]any{subs, nil, gh, mail, tok}},
		{"github", [5]any{subs, repos, nil, mail, tok}},
		{"mailer", [5]any{subs, repos, gh, nil, tok}},
		{"tokens", [5]any{subs, repos, gh, mail, nil}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r, g, m, tg := castDeps(tc.args)
			assert.Panics(t, func() { _ = NewService(s, r, g, m, tg) },
				"expected panic for nil %s", tc.name)
		})
	}
}

func castDeps(args [5]any) (
	subscriptionStore, repoUpserter, githubChecker, confirmationSender, tokenGen,
) {
	asSubs, _ := args[0].(subscriptionStore)
	asRepos, _ := args[1].(repoUpserter)
	asGH, _ := args[2].(githubChecker)
	asMail, _ := args[3].(confirmationSender)
	asTok, _ := args[4].(tokenGen)
	return asSubs, asRepos, asGH, asMail, asTok
}

// --- Subscribe Tests ---

func TestSubscribe_Success(t *testing.T) {
	t.Parallel()
	var createdSub *Subscription
	var sentEmail, sentToken, sentRepo string

	svc := newTestService(
		&mockSubscriptionRepo{
			ExistsFn: func(_ context.Context, _, _, _ string) (bool, error) { return false, nil },
			CreateFn: func(_ context.Context, sub *Subscription) error {
				createdSub = sub
				return nil
			},
		},
		&mockRepoUpserter{
			UpsertFn: func(_ context.Context, _, _ string) error {
				return nil
			},
		},
		&mockGitHubChecker{
			RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return true, nil },
		},
		&mockConfirmationSender{
			SendConfirmationFn: func(_ context.Context, email, token, repo string) error {
				sentEmail = email
				sentToken = token
				sentRepo = repo
				return nil
			},
		},
	)

	require.NoError(t, svc.Subscribe(context.Background(), testEmail, "golang/go"))
	require.NotNil(t, createdSub, "expected subscription to be created")
	assert.Equal(t, testEmail, createdSub.Email)
	assert.Equal(t, "golang", createdSub.RepoOwner)
	assert.Equal(t, "go", createdSub.RepoName)
	assert.Equal(t, StatusPending, createdSub.Status)
	assert.Equal(t, testToken, createdSub.Token)
	assert.Equal(t, testEmail, sentEmail)
	assert.Equal(t, testToken, sentToken)
	assert.Equal(t, "golang/go", sentRepo)
}

func TestSubscribe_InvalidEmail(t *testing.T) {
	t.Parallel()
	svc := newTestService(&mockSubscriptionRepo{}, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	for _, e := range []string{"", "invalid", "@", "foo@", "@bar.com"} {
		t.Run(e, func(t *testing.T) {
			err := svc.Subscribe(context.Background(), e, "golang/go")
			assert.ErrorIs(t, err, ErrInvalidEmail)
		})
	}
}

func TestSubscribe_InvalidRepoFormat(t *testing.T) {
	t.Parallel()
	svc := newTestService(&mockSubscriptionRepo{}, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	for _, r := range []string{"", "noslash", "/", "owner/", "/repo", "a/b/c"} {
		t.Run(r, func(t *testing.T) {
			err := svc.Subscribe(context.Background(), testEmail, r)
			assert.ErrorIs(t, err, ErrInvalidRepo)
		})
	}
}

func TestSubscribe_RepoNotFound(t *testing.T) {
	t.Parallel()
	svc := newTestService(
		&mockSubscriptionRepo{},
		&mockRepoUpserter{},
		&mockGitHubChecker{
			RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return false, nil },
		},
		&mockConfirmationSender{},
	)
	err := svc.Subscribe(context.Background(), testEmail, "nonexistent/repo")
	assert.ErrorIs(t, err, ErrRepoNotFound)
}

func TestSubscribe_AlreadyExists(t *testing.T) {
	t.Parallel()
	t.Run("pre-check detects duplicate", func(t *testing.T) {
		svc := newTestService(
			&mockSubscriptionRepo{
				ExistsFn: func(_ context.Context, _, _, _ string) (bool, error) { return true, nil },
			},
			&mockRepoUpserter{},
			&mockGitHubChecker{
				RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return true, nil },
			},
			&mockConfirmationSender{},
		)
		err := svc.Subscribe(context.Background(), testEmail, "golang/go")
		assert.ErrorIs(t, err, ErrAlreadyExists)
	})

	t.Run("create detects concurrent duplicate", func(t *testing.T) {
		svc := newTestService(
			&mockSubscriptionRepo{
				ExistsFn: func(_ context.Context, _, _, _ string) (bool, error) { return false, nil },
				CreateFn: func(_ context.Context, _ *Subscription) error { return ErrAlreadyExists },
			},
			&mockRepoUpserter{
				UpsertFn: func(_ context.Context, _, _ string) error { return nil },
			},
			&mockGitHubChecker{
				RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return true, nil },
			},
			&mockConfirmationSender{},
		)
		err := svc.Subscribe(context.Background(), testEmail, "golang/go")
		assert.ErrorIs(t, err, ErrAlreadyExists)
	})
}

func TestSubscribe_GitHubAPIError(t *testing.T) {
	t.Parallel()
	svc := newTestService(
		&mockSubscriptionRepo{},
		&mockRepoUpserter{},
		&mockGitHubChecker{
			RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) {
				return false, errors.New("rate limited")
			},
		},
		&mockConfirmationSender{},
	)
	err := svc.Subscribe(context.Background(), testEmail, "golang/go")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrRepoNotFound,
		"GitHub API error must not be wrapped as ErrRepoNotFound")
}

func TestSubscribe_TokenGeneratorFailure_Propagates(t *testing.T) {
	t.Parallel()
	tokenErr := errors.New("entropy source unavailable")
	svc := NewService(
		&mockSubscriptionRepo{
			ExistsFn: func(_ context.Context, _, _, _ string) (bool, error) { return false, nil },
		},
		&mockRepoUpserter{},
		&mockGitHubChecker{
			RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return true, nil },
		},
		&mockConfirmationSender{},
		fixedTokenGenerator{Err: tokenErr},
	)
	err := svc.Subscribe(context.Background(), testEmail, "golang/go")
	require.Error(t, err)
	// errors.Is verifies the chain, not the wrap message — a typo fix to the
	// production string would not falsely fail this test.
	assert.ErrorIs(t, err, tokenErr,
		"underlying token-generator error must be preserved in the chain")
}

func TestSubscribe_UpsertBeforeCreate(t *testing.T) {
	t.Parallel()
	var callOrder []string
	svc := newTestService(
		&mockSubscriptionRepo{
			ExistsFn: func(_ context.Context, _, _, _ string) (bool, error) { return false, nil },
			CreateFn: func(_ context.Context, _ *Subscription) error {
				callOrder = append(callOrder, "create")
				return nil
			},
		},
		&mockRepoUpserter{
			UpsertFn: func(_ context.Context, _, _ string) error {
				callOrder = append(callOrder, "upsert")
				return nil
			},
		},
		&mockGitHubChecker{
			RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return true, nil },
		},
		&mockConfirmationSender{
			SendConfirmationFn: func(_ context.Context, _, _, _ string) error { return nil },
		},
	)
	require.NoError(t, svc.Subscribe(context.Background(), "user@example.com", "golang/go"))
	assert.Equal(t, []string{"upsert", "create"}, callOrder)
}

func TestSubscribe_SMTPFailure_RollsBackSubscription(t *testing.T) {
	t.Parallel()
	var rolledBackID int64
	var rolledBackStatus Status

	svc := newTestService(
		&mockSubscriptionRepo{
			ExistsFn: func(_ context.Context, _, _, _ string) (bool, error) { return false, nil },
			CreateFn: func(_ context.Context, sub *Subscription) error {
				sub.ID = 99
				return nil
			},
			UpdateStatusFn: func(_ context.Context, id int64, status Status) error {
				rolledBackID = id
				rolledBackStatus = status
				return nil
			},
		},
		&mockRepoUpserter{
			UpsertFn: func(_ context.Context, _, _ string) error {
				return nil
			},
		},
		&mockGitHubChecker{
			RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return true, nil },
		},
		&mockConfirmationSender{
			SendConfirmationFn: func(_ context.Context, _, _, _ string) error {
				return errors.New("SMTP connection refused")
			},
		},
	)
	err := svc.Subscribe(context.Background(), testEmail, "golang/go")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmailSendFailed)
	assert.Equal(t, int64(99), rolledBackID)
	assert.Equal(t, StatusUnsubscribed, rolledBackStatus)
}

func TestSubscribe_SMTPFailure_RollbackFailure_JoinedError(t *testing.T) {
	t.Parallel()
	smtpErr := errors.New("smtp down")
	rollbackErr := errors.New("db unavailable")

	var rolledBackID int64
	var rolledBackStatus Status

	svc := newTestService(
		&mockSubscriptionRepo{
			ExistsFn: func(_ context.Context, _, _, _ string) (bool, error) { return false, nil },
			CreateFn: func(_ context.Context, sub *Subscription) error {
				sub.ID = 99
				return nil
			},
			UpdateStatusFn: func(_ context.Context, id int64, status Status) error {
				rolledBackID = id
				rolledBackStatus = status
				return rollbackErr
			},
		},
		&mockRepoUpserter{
			UpsertFn: func(_ context.Context, _, _ string) error { return nil },
		},
		&mockGitHubChecker{
			RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return true, nil },
		},
		&mockConfirmationSender{
			SendConfirmationFn: func(_ context.Context, _, _, _ string) error { return smtpErr },
		},
	)

	err := svc.Subscribe(context.Background(), testEmail, "golang/go")
	require.Error(t, err)
	// errors.Join must surface all three legs: the domain sentinel (callers
	// map to 5xx), the underlying SMTP error, and the rollback failure (tells
	// operators the row is stuck pending). errors.Is on each is enough — no
	// substring match needed.
	assert.ErrorIs(t, err, ErrEmailSendFailed)
	assert.ErrorIs(t, err, smtpErr)
	assert.ErrorIs(t, err, rollbackErr)
	assert.Equal(t, int64(99), rolledBackID, "rollback was still attempted")
	assert.Equal(t, StatusUnsubscribed, rolledBackStatus)
}

// --- Confirm Tests ---

func TestConfirm_Success(t *testing.T) {
	t.Parallel()
	var updatedID int64
	var updatedStatus Status

	svc := newTestService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, token string) (*Subscription, error) {
				return &Subscription{ID: 42, Token: token, Status: StatusPending}, nil
			},
			UpdateStatusFn: func(_ context.Context, id int64, status Status) error {
				updatedID = id
				updatedStatus = status
				return nil
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	require.NoError(t, svc.Confirm(context.Background(), "valid-token"))
	assert.Equal(t, int64(42), updatedID)
	assert.Equal(t, StatusActive, updatedStatus)
}

func TestConfirm_TokenNotFound(t *testing.T) {
	t.Parallel()
	svc := newTestService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*Subscription, error) {
				return nil, ErrNotFound
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	err := svc.Confirm(context.Background(), "invalid-token")
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestConfirm_AlreadyActive_Idempotent(t *testing.T) {
	t.Parallel()
	svc := newTestService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*Subscription, error) {
				return &Subscription{ID: 1, Status: StatusActive}, nil
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	assert.NoError(t, svc.Confirm(context.Background(), "token"),
		"idempotent confirm should return nil")
}

func TestConfirm_UnsubscribedToken(t *testing.T) {
	t.Parallel()
	svc := newTestService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*Subscription, error) {
				return &Subscription{ID: 1, Status: StatusUnsubscribed}, nil
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	err := svc.Confirm(context.Background(), "token")
	assert.ErrorIs(t, err, ErrSubscriptionInactive)
}

func TestConfirm_DBError_Propagates(t *testing.T) {
	t.Parallel()
	svc := newTestService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*Subscription, error) {
				return nil, errors.New("connection refused")
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	err := svc.Confirm(context.Background(), "token")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrTokenNotFound,
		"DB errors must not be wrapped as ErrTokenNotFound")
}

// --- Unsubscribe Tests ---

func TestUnsubscribe_Success(t *testing.T) {
	t.Parallel()
	var updatedStatus Status
	svc := newTestService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*Subscription, error) {
				return &Subscription{ID: 10, Status: StatusActive}, nil
			},
			UpdateStatusFn: func(_ context.Context, _ int64, status Status) error {
				updatedStatus = status
				return nil
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	require.NoError(t, svc.Unsubscribe(context.Background(), "valid-token"))
	assert.Equal(t, StatusUnsubscribed, updatedStatus)
}

func TestUnsubscribe_TokenNotFound(t *testing.T) {
	t.Parallel()
	svc := newTestService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*Subscription, error) {
				return nil, ErrNotFound
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	err := svc.Unsubscribe(context.Background(), "bad-token")
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestUnsubscribe_AlreadyUnsubscribed_Idempotent(t *testing.T) {
	t.Parallel()
	updateCalled := false
	svc := newTestService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*Subscription, error) {
				return &Subscription{ID: 10, Status: StatusUnsubscribed}, nil
			},
			UpdateStatusFn: func(_ context.Context, _ int64, _ Status) error {
				updateCalled = true
				return nil
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	assert.NoError(t, svc.Unsubscribe(context.Background(), "token"))
	assert.False(t, updateCalled,
		"already-unsubscribed should be a no-op — UpdateStatus must not be called")
}

func TestUnsubscribe_DBError_Propagates(t *testing.T) {
	t.Parallel()
	svc := newTestService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*Subscription, error) {
				return nil, errors.New("connection refused")
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	err := svc.Unsubscribe(context.Background(), "token")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrTokenNotFound,
		"DB errors must not be wrapped as ErrTokenNotFound")
}

// --- GetSubscriptions Tests ---

func TestGetSubscriptions_Success(t *testing.T) {
	t.Parallel()
	expected := []Subscription{
		{ID: 1, Email: testEmail, RepoOwner: "golang", RepoName: "go", Status: StatusActive},
	}
	var queriedEmail string
	svc := newTestService(
		&mockSubscriptionRepo{
			GetActiveByEmailFn: func(_ context.Context, email string) ([]Subscription, error) {
				queriedEmail = email
				return expected, nil
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	subs, err := svc.GetSubscriptions(context.Background(), testEmail)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "golang", subs[0].RepoOwner)
	assert.Equal(t, testEmail, queriedEmail)
}

func TestGetSubscriptions_EmptyEmail(t *testing.T) {
	t.Parallel()
	svc := newTestService(&mockSubscriptionRepo{}, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	_, err := svc.GetSubscriptions(context.Background(), "")
	assert.ErrorIs(t, err, ErrInvalidEmail)
}

// --- Email Normalization Tests ---

func TestSubscribe_NormalizesEmail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"USER@Example.COM", "user@example.com"},
		{"Alice <alice@example.com>", "alice@example.com"},
		{"  Bob <BOB@Test.Org>  ", "bob@test.org"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var storedEmail string
			svc := newTestService(
				&mockSubscriptionRepo{
					ExistsFn: func(_ context.Context, _, _, _ string) (bool, error) { return false, nil },
					CreateFn: func(_ context.Context, sub *Subscription) error {
						storedEmail = sub.Email
						return nil
					},
				},
				&mockRepoUpserter{
					UpsertFn: func(_ context.Context, _, _ string) error {
						return nil
					},
				},
				&mockGitHubChecker{
					RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return true, nil },
				},
				&mockConfirmationSender{
					SendConfirmationFn: func(_ context.Context, _, _, _ string) error { return nil },
				},
			)
			require.NoError(t, svc.Subscribe(context.Background(), tt.input, "golang/go"))
			assert.Equal(t, tt.want, storedEmail)
		})
	}
}

func TestGetSubscriptions_NormalizesEmail(t *testing.T) {
	t.Parallel()
	var queriedEmail string
	svc := newTestService(
		&mockSubscriptionRepo{
			GetActiveByEmailFn: func(_ context.Context, email string) ([]Subscription, error) {
				queriedEmail = email
				return []Subscription{}, nil
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	_, err := svc.GetSubscriptions(context.Background(), "USER@Example.COM")
	require.NoError(t, err)
	assert.Equal(t, testEmail, queriedEmail)
}
