//nolint:testpackage // white-box tests share unexported helpers
package subscription

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	testEmail   = "user@example.com"
	testToken   = "test-token-deterministic"
	testBaseURL = "http://test.local"
)

func newTestService(
	subs *mockSubscriptionRepo,
	repos *mockRepoUpserter,
	gh *mockGitHubChecker,
	mail *mockConfirmationSender,
) *Service {
	svc, err := NewService(subs, repos, gh, mail, fixedTokenGenerator{Token: testToken},
		NewConfirmLinkBuilder(testBaseURL))
	if err != nil {
		panic(err) // test wiring is always complete
	}
	return svc
}

func TestNewService_ErrorsOnNilDependency(t *testing.T) {
	t.Parallel()
	subs := &mockSubscriptionRepo{}
	repos := &mockRepoUpserter{}
	gh := &mockGitHubChecker{}
	mail := &mockConfirmationSender{}
	tok := fixedTokenGenerator{Token: testToken}
	links := NewConfirmLinkBuilder(testBaseURL)

	cases := []struct {
		name string
		args [6]any
	}{
		{"subs", [6]any{nil, repos, gh, mail, tok, links}},
		{"repos", [6]any{subs, nil, gh, mail, tok, links}},
		{"github", [6]any{subs, repos, nil, mail, tok, links}},
		{"mailer", [6]any{subs, repos, gh, nil, tok, links}},
		{"tokens", [6]any{subs, repos, gh, mail, nil, links}},
		{"links", [6]any{subs, repos, gh, mail, tok, nil}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r, g, m, tg, l := castDeps(tc.args)
			_, err := NewService(s, r, g, m, tg, l)
			assert.Error(t, err, "expected error for nil %s", tc.name)
		})
	}
}

func castDeps(args [6]any) (
	subscriptionStore, repoUpserter, githubChecker, confirmationSender, tokenGen, confirmationLinkBuilder,
) {
	asSubs, _ := args[0].(subscriptionStore)
	asRepos, _ := args[1].(repoUpserter)
	asGH, _ := args[2].(githubChecker)
	asMail, _ := args[3].(confirmationSender)
	asTok, _ := args[4].(tokenGen)
	asLinks, _ := args[5].(confirmationLinkBuilder)
	return asSubs, asRepos, asGH, asMail, asTok, asLinks
}

func TestSubscribe_Success(t *testing.T) {
	t.Parallel()
	var createdSub *Subscription
	var sentEmail, sentConfirmURL, sentRepo string

	subs := &mockSubscriptionRepo{}
	subs.On("GetByEmailAndRepo", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, ErrNotFound)
	subs.On("Create", mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		createdSub, _ = args.Get(1).(*Subscription)
	})
	repos := &mockRepoUpserter{}
	repos.On("Upsert", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	gh := &mockGitHubChecker{}
	gh.On("RepoExists", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mail := &mockConfirmationSender{}
	mail.On("SendConfirmation", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Run(func(args mock.Arguments) {
		sentEmail, sentConfirmURL, sentRepo = args.String(1), args.String(2), args.String(3)
	})

	svc := newTestService(subs, repos, gh, mail)

	require.NoError(t, svc.Subscribe(context.Background(), testEmail, "golang/go"))
	require.NotNil(t, createdSub, "expected subscription to be created")
	assert.Equal(t, testEmail, createdSub.Email)
	assert.Equal(t, "golang", createdSub.RepoOwner)
	assert.Equal(t, "go", createdSub.RepoName)
	assert.Equal(t, StatusPending, createdSub.Status)
	assert.Equal(t, testToken, createdSub.Token)
	assert.Equal(t, testEmail, sentEmail)
	assert.Equal(t, testBaseURL+"/api/confirm/"+testToken, sentConfirmURL)
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
	gh := &mockGitHubChecker{}
	gh.On("RepoExists", mock.Anything, mock.Anything, mock.Anything).Return(false, nil)

	svc := newTestService(&mockSubscriptionRepo{}, &mockRepoUpserter{}, gh, &mockConfirmationSender{})
	err := svc.Subscribe(context.Background(), testEmail, "nonexistent/repo")
	assert.ErrorIs(t, err, ErrRepoNotFound)
}

func TestSubscribe_AlreadyExists(t *testing.T) {
	t.Parallel()
	t.Run("pre-check detects active duplicate", func(t *testing.T) {
		subs := &mockSubscriptionRepo{}
		subs.On("GetByEmailAndRepo", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(&Subscription{ID: 1, Status: StatusActive}, nil)
		gh := &mockGitHubChecker{}
		gh.On("RepoExists", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)

		svc := newTestService(subs, &mockRepoUpserter{}, gh, &mockConfirmationSender{})
		err := svc.Subscribe(context.Background(), testEmail, "golang/go")
		assert.ErrorIs(t, err, ErrAlreadyExists)
	})

	t.Run("create detects concurrent duplicate", func(t *testing.T) {
		subs := &mockSubscriptionRepo{}
		subs.On("GetByEmailAndRepo", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(nil, ErrNotFound)
		subs.On("Create", mock.Anything, mock.Anything).Return(ErrAlreadyExists)
		repos := &mockRepoUpserter{}
		repos.On("Upsert", mock.Anything, mock.Anything, mock.Anything).Return(nil)
		gh := &mockGitHubChecker{}
		gh.On("RepoExists", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)

		svc := newTestService(subs, repos, gh, &mockConfirmationSender{})
		err := svc.Subscribe(context.Background(), testEmail, "golang/go")
		assert.ErrorIs(t, err, ErrAlreadyExists)
	})
}

// A re-subscribe over a still-pending row (e.g. a confirmation that was published
// but never delivered) must refresh the token in place and resend, not 409.
func TestSubscribe_RefreshesPendingSubscription(t *testing.T) {
	t.Parallel()
	const newToken = "refreshed-token"

	var updatedID int64
	var updatedToken, sentConfirmURL string

	subs := &mockSubscriptionRepo{}
	subs.On("GetByEmailAndRepo", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&Subscription{
			ID: 7, Email: testEmail, RepoOwner: "golang", RepoName: "go", Status: StatusPending,
		}, nil)
	subs.On("UpdateToken", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).
		Run(func(args mock.Arguments) {
			updatedID, _ = args.Get(1).(int64)
			updatedToken = args.String(3)
		})
	gh := &mockGitHubChecker{}
	gh.On("RepoExists", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mail := &mockConfirmationSender{}
	mail.On("SendConfirmation", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Run(func(args mock.Arguments) {
		sentConfirmURL = args.String(2)
	})

	svc, err := NewService(subs, &mockRepoUpserter{}, gh, mail,
		fixedTokenGenerator{Token: newToken}, NewConfirmLinkBuilder(testBaseURL))
	require.NoError(t, err)

	require.NoError(t, svc.Subscribe(context.Background(), testEmail, "golang/go"))
	assert.Equal(t, int64(7), updatedID, "the existing pending row is updated in place")
	assert.Equal(t, newToken, updatedToken)
	assert.Equal(t, testBaseURL+"/api/confirm/"+newToken, sentConfirmURL,
		"confirmation is resent with the new token")
	subs.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

// Two concurrent re-subscribes over the same pending row race on UpdateToken's
// CAS guard; the loser must not silently overwrite the winner's token or email
// out a confirm link for a token that was never actually written.
func TestSubscribe_RefreshRaceLoserGetsAlreadyExists(t *testing.T) {
	t.Parallel()

	subs := &mockSubscriptionRepo{}
	subs.On("GetByEmailAndRepo", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&Subscription{
			ID: 7, Email: testEmail, RepoOwner: "golang", RepoName: "go",
			Token: "stale-token", Status: StatusPending,
		}, nil)
	subs.On("UpdateToken", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(ErrNotFound)
	gh := &mockGitHubChecker{}
	gh.On("RepoExists", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mail := &mockConfirmationSender{}

	svc := newTestService(subs, &mockRepoUpserter{}, gh, mail)
	err := svc.Subscribe(context.Background(), testEmail, "golang/go")
	assert.ErrorIs(t, err, ErrAlreadyExists)
	mail.AssertNotCalled(t, "SendConfirmation",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestSubscribe_GitHubAPIError(t *testing.T) {
	t.Parallel()
	gh := &mockGitHubChecker{}
	gh.On("RepoExists", mock.Anything, mock.Anything, mock.Anything).
		Return(false, errors.New("rate limited"))

	svc := newTestService(&mockSubscriptionRepo{}, &mockRepoUpserter{}, gh, &mockConfirmationSender{})
	err := svc.Subscribe(context.Background(), testEmail, "golang/go")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrRepoNotFound,
		"GitHub API error must not be wrapped as ErrRepoNotFound")
}

func TestSubscribe_TokenGeneratorFailure_Propagates(t *testing.T) {
	t.Parallel()
	tokenErr := errors.New("entropy source unavailable")
	subs := &mockSubscriptionRepo{}
	subs.On("GetByEmailAndRepo", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, ErrNotFound)
	gh := &mockGitHubChecker{}
	gh.On("RepoExists", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)

	svc, err := NewService(subs, &mockRepoUpserter{}, gh, &mockConfirmationSender{},
		fixedTokenGenerator{Err: tokenErr}, NewConfirmLinkBuilder(testBaseURL))
	require.NoError(t, err)

	err = svc.Subscribe(context.Background(), testEmail, "golang/go")
	require.Error(t, err)
	assert.ErrorIs(t, err, tokenErr,
		"underlying token-generator error must be preserved in the chain")
}

func TestSubscribe_UpsertBeforeCreate(t *testing.T) {
	t.Parallel()
	var callOrder []string

	subs := &mockSubscriptionRepo{}
	subs.On("GetByEmailAndRepo", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, ErrNotFound)
	subs.On("Create", mock.Anything, mock.Anything).Return(nil).Run(func(_ mock.Arguments) {
		callOrder = append(callOrder, "create")
	})
	repos := &mockRepoUpserter{}
	repos.On("Upsert", mock.Anything, mock.Anything, mock.Anything).Return(nil).Run(func(_ mock.Arguments) {
		callOrder = append(callOrder, "upsert")
	})
	gh := &mockGitHubChecker{}
	gh.On("RepoExists", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mail := &mockConfirmationSender{}
	mail.On("SendConfirmation", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	svc := newTestService(subs, repos, gh, mail)
	require.NoError(t, svc.Subscribe(context.Background(), "user@example.com", "golang/go"))
	assert.Equal(t, []string{"upsert", "create"}, callOrder)
}

func TestSubscribe_SMTPFailure_RollsBackSubscription(t *testing.T) {
	t.Parallel()
	var rolledBackID int64
	var rolledBackStatus Status

	subs := &mockSubscriptionRepo{}
	subs.On("GetByEmailAndRepo", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, ErrNotFound)
	subs.On("Create", mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		if sub, ok := args.Get(1).(*Subscription); ok {
			sub.ID = 99
		}
	})
	subs.On("UpdateStatus", mock.Anything, mock.Anything, mock.Anything).Return(nil).
		Run(func(args mock.Arguments) {
			rolledBackID, _ = args.Get(1).(int64)
			rolledBackStatus, _ = args.Get(2).(Status)
		})
	repos := &mockRepoUpserter{}
	repos.On("Upsert", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	gh := &mockGitHubChecker{}
	gh.On("RepoExists", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mail := &mockConfirmationSender{}
	mail.On("SendConfirmation", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(errors.New("SMTP connection refused"))

	svc := newTestService(subs, repos, gh, mail)
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

	subs := &mockSubscriptionRepo{}
	subs.On("GetByEmailAndRepo", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, ErrNotFound)
	subs.On("Create", mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		if sub, ok := args.Get(1).(*Subscription); ok {
			sub.ID = 99
		}
	})
	subs.On("UpdateStatus", mock.Anything, mock.Anything, mock.Anything).Return(rollbackErr).
		Run(func(args mock.Arguments) {
			rolledBackID, _ = args.Get(1).(int64)
			rolledBackStatus, _ = args.Get(2).(Status)
		})
	repos := &mockRepoUpserter{}
	repos.On("Upsert", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	gh := &mockGitHubChecker{}
	gh.On("RepoExists", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mail := &mockConfirmationSender{}
	mail.On("SendConfirmation", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(smtpErr)

	svc := newTestService(subs, repos, gh, mail)
	err := svc.Subscribe(context.Background(), testEmail, "golang/go")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmailSendFailed)
	assert.ErrorIs(t, err, smtpErr)
	assert.ErrorIs(t, err, rollbackErr)
	assert.Equal(t, int64(99), rolledBackID, "rollback was still attempted")
	assert.Equal(t, StatusUnsubscribed, rolledBackStatus)
}

func TestConfirm_Success(t *testing.T) {
	t.Parallel()
	var updatedID int64
	var updatedStatus Status

	subs := &mockSubscriptionRepo{}
	subs.On("GetByToken", mock.Anything, mock.Anything).
		Return(&Subscription{ID: 42, Status: StatusPending}, nil)
	subs.On("UpdateStatus", mock.Anything, mock.Anything, mock.Anything).Return(nil).
		Run(func(args mock.Arguments) {
			updatedID, _ = args.Get(1).(int64)
			updatedStatus, _ = args.Get(2).(Status)
		})

	svc := newTestService(subs, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	require.NoError(t, svc.Confirm(context.Background(), "valid-token"))
	assert.Equal(t, int64(42), updatedID)
	assert.Equal(t, StatusActive, updatedStatus)
}

func TestConfirm_TokenNotFound(t *testing.T) {
	t.Parallel()
	subs := &mockSubscriptionRepo{}
	subs.On("GetByToken", mock.Anything, mock.Anything).Return(nil, ErrNotFound)

	svc := newTestService(subs, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	err := svc.Confirm(context.Background(), "invalid-token")
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestConfirm_AlreadyActive_Idempotent(t *testing.T) {
	t.Parallel()
	subs := &mockSubscriptionRepo{}
	subs.On("GetByToken", mock.Anything, mock.Anything).
		Return(&Subscription{ID: 1, Status: StatusActive}, nil)

	svc := newTestService(subs, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	assert.NoError(t, svc.Confirm(context.Background(), "token"), "idempotent confirm should return nil")
	subs.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything)
}

func TestConfirm_UnsubscribedToken(t *testing.T) {
	t.Parallel()
	subs := &mockSubscriptionRepo{}
	subs.On("GetByToken", mock.Anything, mock.Anything).
		Return(&Subscription{ID: 1, Status: StatusUnsubscribed}, nil)

	svc := newTestService(subs, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	err := svc.Confirm(context.Background(), "token")
	assert.ErrorIs(t, err, ErrSubscriptionInactive)
}

func TestConfirm_DBError_Propagates(t *testing.T) {
	t.Parallel()
	subs := &mockSubscriptionRepo{}
	subs.On("GetByToken", mock.Anything, mock.Anything).Return(nil, errors.New("connection refused"))

	svc := newTestService(subs, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	err := svc.Confirm(context.Background(), "token")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrTokenNotFound, "DB errors must not be wrapped as ErrTokenNotFound")
}

func TestUnsubscribe_Success(t *testing.T) {
	t.Parallel()
	var updatedStatus Status

	subs := &mockSubscriptionRepo{}
	subs.On("GetByToken", mock.Anything, mock.Anything).
		Return(&Subscription{ID: 10, Status: StatusActive}, nil)
	subs.On("UpdateStatus", mock.Anything, mock.Anything, mock.Anything).Return(nil).
		Run(func(args mock.Arguments) {
			updatedStatus, _ = args.Get(2).(Status)
		})

	svc := newTestService(subs, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	require.NoError(t, svc.Unsubscribe(context.Background(), "valid-token"))
	assert.Equal(t, StatusUnsubscribed, updatedStatus)
}

func TestUnsubscribe_TokenNotFound(t *testing.T) {
	t.Parallel()
	subs := &mockSubscriptionRepo{}
	subs.On("GetByToken", mock.Anything, mock.Anything).Return(nil, ErrNotFound)

	svc := newTestService(subs, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	err := svc.Unsubscribe(context.Background(), "bad-token")
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestUnsubscribe_AlreadyUnsubscribed_Idempotent(t *testing.T) {
	t.Parallel()
	subs := &mockSubscriptionRepo{}
	subs.On("GetByToken", mock.Anything, mock.Anything).
		Return(&Subscription{ID: 10, Status: StatusUnsubscribed}, nil)

	svc := newTestService(subs, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	assert.NoError(t, svc.Unsubscribe(context.Background(), "token"))
	subs.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything)
}

func TestUnsubscribe_DBError_Propagates(t *testing.T) {
	t.Parallel()
	subs := &mockSubscriptionRepo{}
	subs.On("GetByToken", mock.Anything, mock.Anything).Return(nil, errors.New("connection refused"))

	svc := newTestService(subs, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	err := svc.Unsubscribe(context.Background(), "token")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrTokenNotFound, "DB errors must not be wrapped as ErrTokenNotFound")
}

func TestGetSubscriptions_Success(t *testing.T) {
	t.Parallel()
	expected := []Subscription{
		{ID: 1, Email: testEmail, RepoOwner: "golang", RepoName: "go", Status: StatusActive},
	}
	var queriedEmail string

	subs := &mockSubscriptionRepo{}
	subs.On("GetActiveByEmail", mock.Anything, mock.Anything).Return(expected, nil).
		Run(func(args mock.Arguments) {
			queriedEmail = args.String(1)
		})

	svc := newTestService(subs, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	got, err := svc.GetSubscriptions(context.Background(), testEmail)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "golang", got[0].RepoOwner)
	assert.Equal(t, testEmail, queriedEmail)
}

func TestGetSubscriptions_EmptyEmail(t *testing.T) {
	t.Parallel()
	svc := newTestService(&mockSubscriptionRepo{}, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	_, err := svc.GetSubscriptions(context.Background(), "")
	assert.ErrorIs(t, err, ErrInvalidEmail)
}

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

			subs := &mockSubscriptionRepo{}
			subs.On("GetByEmailAndRepo", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Return(nil, ErrNotFound)
			subs.On("Create", mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
				if sub, ok := args.Get(1).(*Subscription); ok {
					storedEmail = sub.Email
				}
			})
			repos := &mockRepoUpserter{}
			repos.On("Upsert", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			gh := &mockGitHubChecker{}
			gh.On("RepoExists", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
			mail := &mockConfirmationSender{}
			mail.On("SendConfirmation", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

			svc := newTestService(subs, repos, gh, mail)
			require.NoError(t, svc.Subscribe(context.Background(), tt.input, "golang/go"))
			assert.Equal(t, tt.want, storedEmail)
		})
	}
}

func TestGetSubscriptions_NormalizesEmail(t *testing.T) {
	t.Parallel()
	var queriedEmail string

	subs := &mockSubscriptionRepo{}
	subs.On("GetActiveByEmail", mock.Anything, mock.Anything).Return([]Subscription{}, nil).
		Run(func(args mock.Arguments) {
			queriedEmail = args.String(1)
		})

	svc := newTestService(subs, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	_, err := svc.GetSubscriptions(context.Background(), "USER@Example.COM")
	require.NoError(t, err)
	assert.Equal(t, testEmail, queriedEmail)
}
