//nolint:testpackage // white-box tests share unexported helpers
package subscription

import (
	"context"
	"errors"
	"strings"
	"testing"
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

// --- Subscribe Tests ---

func TestSubscribe_Success(t *testing.T) {
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

	if err := svc.Subscribe(context.Background(), testEmail, "golang/go"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if createdSub == nil {
		t.Fatal("expected subscription to be created")
	}
	if createdSub.Email != testEmail {
		t.Errorf("email = %q, want %q", createdSub.Email, testEmail)
	}
	if createdSub.RepoOwner != "golang" || createdSub.RepoName != "go" {
		t.Errorf("repo = %s/%s, want golang/go", createdSub.RepoOwner, createdSub.RepoName)
	}
	if createdSub.Status != StatusPending {
		t.Errorf("status = %q, want %q", createdSub.Status, StatusPending)
	}
	if createdSub.Token != testToken {
		t.Errorf("token = %q, want %q", createdSub.Token, testToken)
	}
	if sentEmail != testEmail {
		t.Errorf("confirmation email sent to %q, want %q", sentEmail, testEmail)
	}
	if sentToken != testToken {
		t.Errorf("confirmation token = %q, want %q", sentToken, testToken)
	}
	if sentRepo != "golang/go" {
		t.Errorf("confirmation repo = %q, want %q", sentRepo, "golang/go")
	}
}

func TestSubscribe_InvalidEmail(t *testing.T) {
	svc := newTestService(&mockSubscriptionRepo{}, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	for _, e := range []string{"", "invalid", "@", "foo@", "@bar.com"} {
		if err := svc.Subscribe(context.Background(), e, "golang/go"); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("Subscribe(%q): got %v, want ErrInvalidEmail", e, err)
		}
	}
}

func TestSubscribe_InvalidRepoFormat(t *testing.T) {
	svc := newTestService(&mockSubscriptionRepo{}, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	for _, r := range []string{"", "noslash", "/", "owner/", "/repo", "a/b/c"} {
		if err := svc.Subscribe(context.Background(), testEmail, r); !errors.Is(err, ErrInvalidRepo) {
			t.Errorf("Subscribe(repo=%q): got %v, want ErrInvalidRepo", r, err)
		}
	}
}

func TestSubscribe_RepoNotFound(t *testing.T) {
	svc := newTestService(
		&mockSubscriptionRepo{},
		&mockRepoUpserter{},
		&mockGitHubChecker{
			RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return false, nil },
		},
		&mockConfirmationSender{},
	)
	if err := svc.Subscribe(context.Background(), testEmail, "nonexistent/repo"); !errors.Is(err, ErrRepoNotFound) {
		t.Errorf("got %v, want ErrRepoNotFound", err)
	}
}

func TestSubscribe_AlreadyExists(t *testing.T) {
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
		if err := svc.Subscribe(context.Background(), testEmail, "golang/go"); !errors.Is(err, ErrAlreadyExists) {
			t.Errorf("got %v, want ErrAlreadyExists", err)
		}
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
		if err := svc.Subscribe(context.Background(), testEmail, "golang/go"); !errors.Is(err, ErrAlreadyExists) {
			t.Errorf("got %v, want ErrAlreadyExists", err)
		}
	})
}

func TestSubscribe_GitHubAPIError(t *testing.T) {
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
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrRepoNotFound) {
		t.Error("should not wrap GitHub API error as ErrRepoNotFound")
	}
}

func TestSubscribe_TokenGeneratorFailure_Propagates(t *testing.T) {
	svc := NewService(
		&mockSubscriptionRepo{
			ExistsFn: func(_ context.Context, _, _, _ string) (bool, error) { return false, nil },
		},
		&mockRepoUpserter{},
		&mockGitHubChecker{
			RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return true, nil },
		},
		&mockConfirmationSender{},
		fixedTokenGenerator{Err: errors.New("entropy source unavailable")},
	)
	err := svc.Subscribe(context.Background(), testEmail, "golang/go")
	if err == nil {
		t.Fatal("expected token generator error to propagate")
	}
	if !strings.Contains(err.Error(), "generating token") {
		t.Errorf("error = %q, want it to mention token generation", err.Error())
	}
}

func TestSubscribe_UpsertBeforeCreate(t *testing.T) {
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
	if err := svc.Subscribe(context.Background(), "user@example.com", "golang/go"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(callOrder) != 2 || callOrder[0] != "upsert" || callOrder[1] != "create" {
		t.Errorf("call order = %v, want [upsert, create]", callOrder)
	}
}

func TestSubscribe_SMTPFailure_RollsBackSubscription(t *testing.T) {
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
	if err == nil {
		t.Fatal("expected error from SMTP failure")
	}
	if !errors.Is(err, ErrEmailSendFailed) {
		t.Errorf("expected ErrEmailSendFailed, got: %v", err)
	}
	if rolledBackID != 99 {
		t.Errorf("rollback ID = %d, want 99", rolledBackID)
	}
	if rolledBackStatus != StatusUnsubscribed {
		t.Errorf("rollback status = %q, want %q", rolledBackStatus, StatusUnsubscribed)
	}
}

// --- Confirm Tests ---

func TestConfirm_Success(t *testing.T) {
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
	if err := svc.Confirm(context.Background(), "valid-token"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updatedID != 42 {
		t.Errorf("updated ID = %d, want 42", updatedID)
	}
	if updatedStatus != StatusActive {
		t.Errorf("updated status = %q, want %q", updatedStatus, StatusActive)
	}
}

func TestConfirm_TokenNotFound(t *testing.T) {
	svc := newTestService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*Subscription, error) {
				return nil, ErrNotFound
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	if err := svc.Confirm(context.Background(), "invalid-token"); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("got %v, want ErrTokenNotFound", err)
	}
}

func TestConfirm_AlreadyActive_Idempotent(t *testing.T) {
	svc := newTestService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*Subscription, error) {
				return &Subscription{ID: 1, Status: StatusActive}, nil
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	if err := svc.Confirm(context.Background(), "token"); err != nil {
		t.Errorf("got %v, want nil (idempotent confirm)", err)
	}
}

func TestConfirm_UnsubscribedToken(t *testing.T) {
	svc := newTestService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*Subscription, error) {
				return &Subscription{ID: 1, Status: StatusUnsubscribed}, nil
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	if err := svc.Confirm(context.Background(), "token"); !errors.Is(err, ErrSubscriptionInactive) {
		t.Errorf("got %v, want ErrSubscriptionInactive", err)
	}
}

func TestConfirm_DBError_Propagates(t *testing.T) {
	svc := newTestService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*Subscription, error) {
				return nil, errors.New("connection refused")
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	err := svc.Confirm(context.Background(), "token")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrTokenNotFound) {
		t.Error("DB errors should not be wrapped as ErrTokenNotFound")
	}
}

// --- Unsubscribe Tests ---

func TestUnsubscribe_Success(t *testing.T) {
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
	if err := svc.Unsubscribe(context.Background(), "valid-token"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updatedStatus != StatusUnsubscribed {
		t.Errorf("status = %q, want %q", updatedStatus, StatusUnsubscribed)
	}
}

func TestUnsubscribe_TokenNotFound(t *testing.T) {
	svc := newTestService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*Subscription, error) {
				return nil, ErrNotFound
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	if err := svc.Unsubscribe(context.Background(), "bad-token"); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("got %v, want ErrTokenNotFound", err)
	}
}

func TestUnsubscribe_AlreadyUnsubscribed_Idempotent(t *testing.T) {
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
	if err := svc.Unsubscribe(context.Background(), "token"); err != nil {
		t.Errorf("got %v, want nil (idempotent unsubscribe)", err)
	}
	if updateCalled {
		t.Error("should not call UpdateStatus for already-unsubscribed subscription")
	}
}

func TestUnsubscribe_DBError_Propagates(t *testing.T) {
	svc := newTestService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*Subscription, error) {
				return nil, errors.New("connection refused")
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	err := svc.Unsubscribe(context.Background(), "token")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrTokenNotFound) {
		t.Error("DB errors should not be wrapped as ErrTokenNotFound")
	}
}

// --- GetSubscriptions Tests ---

func TestGetSubscriptions_Success(t *testing.T) {
	expected := []Subscription{
		{ID: 1, Email: testEmail, RepoOwner: "golang", RepoName: "go", Status: StatusActive},
	}
	svc := newTestService(
		&mockSubscriptionRepo{
			GetActiveByEmailFn: func(_ context.Context, email string) ([]Subscription, error) {
				if email != testEmail {
					t.Errorf("queried email = %q, want %q", email, testEmail)
				}
				return expected, nil
			},
		},
		&mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{},
	)
	subs, err := svc.GetSubscriptions(context.Background(), testEmail)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("got %d subscriptions, want 1", len(subs))
	}
	if subs[0].RepoOwner != "golang" {
		t.Errorf("repo_owner = %q, want %q", subs[0].RepoOwner, "golang")
	}
}

func TestGetSubscriptions_EmptyEmail(t *testing.T) {
	svc := newTestService(&mockSubscriptionRepo{}, &mockRepoUpserter{}, &mockGitHubChecker{}, &mockConfirmationSender{})
	if _, err := svc.GetSubscriptions(context.Background(), ""); !errors.Is(err, ErrInvalidEmail) {
		t.Errorf("got %v, want ErrInvalidEmail", err)
	}
}

// --- Email Normalization Tests ---

func TestSubscribe_NormalizesEmail(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"USER@Example.COM", "user@example.com"},
		{"Alice <alice@example.com>", "alice@example.com"},
		{"  Bob <BOB@Test.Org>  ", "bob@test.org"},
	}
	for _, tt := range tests {
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
		if err := svc.Subscribe(context.Background(), tt.input, "golang/go"); err != nil {
			t.Fatalf("Subscribe(%q): unexpected error: %v", tt.input, err)
		}
		if storedEmail != tt.want {
			t.Errorf("Subscribe(%q): stored email = %q, want %q", tt.input, storedEmail, tt.want)
		}
	}
}

func TestGetSubscriptions_NormalizesEmail(t *testing.T) {
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
	if _, err := svc.GetSubscriptions(context.Background(), "USER@Example.COM"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if queriedEmail != testEmail {
		t.Errorf("queried email = %q, want %q", queriedEmail, testEmail)
	}
}
