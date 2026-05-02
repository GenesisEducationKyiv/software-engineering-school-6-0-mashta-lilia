package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github-release-notifier/internal/model"
	"github-release-notifier/internal/repository"
)

func newTestSubscriptionService(
	subs *mockSubscriptionRepo,
	repos *mockRepoStore,
	gh *mockGitHubClient,
	mail *mockMailer,
) *SubscriptionService {
	return NewSubscriptionService(subs, repos, gh, mail)
}

const testEmail = "user@example.com"

// --- Subscribe Tests ---

func TestSubscribe_Success(t *testing.T) {
	var createdSub *model.Subscription
	var sentEmail, sentToken, sentRepo string

	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{
			ExistsFn: func(_ context.Context, _, _, _ string) (bool, error) { return false, nil },
			CreateFn: func(_ context.Context, sub *model.Subscription) error {
				createdSub = sub
				return nil
			},
		},
		&mockRepoStore{
			UpsertFn: func(_ context.Context, _, _ string) (*model.TrackedRepository, error) {
				return &model.TrackedRepository{ID: 1}, nil
			},
		},
		&mockGitHubClient{
			RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return true, nil },
		},
		&mockMailer{
			SendConfirmationFn: func(_ context.Context, email, token, repo string) error {
				sentEmail = email
				sentToken = token
				sentRepo = repo
				return nil
			},
		},
	)

	err := svc.Subscribe(context.Background(), testEmail, "golang/go")
	if err != nil {
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
	if createdSub.Status != model.StatusPending {
		t.Errorf("status = %q, want %q", createdSub.Status, model.StatusPending)
	}
	if createdSub.Token == "" {
		t.Error("expected token to be generated")
	}
	if sentEmail != testEmail {
		t.Errorf("confirmation email sent to %q, want %q", sentEmail, testEmail)
	}
	if sentToken != createdSub.Token {
		t.Error("confirmation token mismatch")
	}
	if sentRepo != "golang/go" {
		t.Errorf("confirmation repo = %q, want %q", sentRepo, "golang/go")
	}
}

func TestSubscribe_InvalidEmail(t *testing.T) {
	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{},
		&mockRepoStore{},
		&mockGitHubClient{},
		&mockMailer{},
	)

	tests := []string{"", "invalid", "@", "foo@", "@bar.com"}
	for _, email := range tests {
		err := svc.Subscribe(context.Background(), email, "golang/go")
		if !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("Subscribe(%q): got %v, want ErrInvalidEmail", email, err)
		}
	}
}

func TestSubscribe_InvalidRepoFormat(t *testing.T) {
	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{},
		&mockRepoStore{},
		&mockGitHubClient{},
		&mockMailer{},
	)

	tests := []string{"", "noslash", "/", "owner/", "/repo", "a/b/c"}
	for _, repo := range tests {
		err := svc.Subscribe(context.Background(), testEmail, repo)
		if !errors.Is(err, ErrInvalidRepo) {
			t.Errorf("Subscribe(repo=%q): got %v, want ErrInvalidRepo", repo, err)
		}
	}
}

func TestSubscribe_RepoNotFound(t *testing.T) {
	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{},
		&mockRepoStore{},
		&mockGitHubClient{
			RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return false, nil },
		},
		&mockMailer{},
	)

	err := svc.Subscribe(context.Background(), testEmail, "nonexistent/repo")
	if !errors.Is(err, ErrRepoNotFound) {
		t.Errorf("got %v, want ErrRepoNotFound", err)
	}
}

func TestSubscribe_AlreadyExists(t *testing.T) {
	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{
			ExistsFn: func(_ context.Context, _, _, _ string) (bool, error) { return true, nil },
		},
		&mockRepoStore{},
		&mockGitHubClient{
			RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return true, nil },
		},
		&mockMailer{},
	)

	err := svc.Subscribe(context.Background(), testEmail, "golang/go")
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("got %v, want ErrAlreadyExists", err)
	}
}

func TestSubscribe_GitHubAPIError(t *testing.T) {
	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{},
		&mockRepoStore{},
		&mockGitHubClient{
			RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) {
				return false, errors.New("rate limited")
			},
		},
		&mockMailer{},
	)

	err := svc.Subscribe(context.Background(), testEmail, "golang/go")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrRepoNotFound) {
		t.Error("should not wrap GitHub API error as ErrRepoNotFound")
	}
}

func TestSubscribe_UpsertBeforeCreate(t *testing.T) {
	// Verify FK ordering: Upsert must be called before Create
	var callOrder []string

	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{
			ExistsFn: func(_ context.Context, _, _, _ string) (bool, error) { return false, nil },
			CreateFn: func(_ context.Context, _ *model.Subscription) error {
				callOrder = append(callOrder, "create")
				return nil
			},
		},
		&mockRepoStore{
			UpsertFn: func(_ context.Context, _, _ string) (*model.TrackedRepository, error) {
				callOrder = append(callOrder, "upsert")
				return &model.TrackedRepository{ID: 1}, nil
			},
		},
		&mockGitHubClient{
			RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return true, nil },
		},
		&mockMailer{
			SendConfirmationFn: func(_ context.Context, _, _, _ string) error { return nil },
		},
	)

	err := svc.Subscribe(context.Background(), "user@example.com", "golang/go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(callOrder) != 2 || callOrder[0] != "upsert" || callOrder[1] != "create" {
		t.Errorf("call order = %v, want [upsert, create]", callOrder)
	}
}

func TestSubscribe_SMTPFailure_RollsBackSubscription(t *testing.T) {
	var rolledBackID int64
	var rolledBackStatus model.SubscriptionStatus

	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{
			ExistsFn: func(_ context.Context, _, _, _ string) (bool, error) { return false, nil },
			CreateFn: func(_ context.Context, sub *model.Subscription) error {
				sub.ID = 99
				return nil
			},
			UpdateStatusFn: func(_ context.Context, id int64, status model.SubscriptionStatus) error {
				rolledBackID = id
				rolledBackStatus = status
				return nil
			},
		},
		&mockRepoStore{
			UpsertFn: func(_ context.Context, _, _ string) (*model.TrackedRepository, error) {
				return &model.TrackedRepository{ID: 1}, nil
			},
		},
		&mockGitHubClient{
			RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return true, nil },
		},
		&mockMailer{
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
	if rolledBackStatus != model.StatusUnsubscribed {
		t.Errorf("rollback status = %q, want %q", rolledBackStatus, model.StatusUnsubscribed)
	}
}

// --- Confirm Tests ---

func TestConfirm_Success(t *testing.T) {
	var updatedID int64
	var updatedStatus model.SubscriptionStatus

	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, token string) (*model.Subscription, error) {
				return &model.Subscription{ID: 42, Token: token, Status: model.StatusPending}, nil
			},
			UpdateStatusFn: func(_ context.Context, id int64, status model.SubscriptionStatus) error {
				updatedID = id
				updatedStatus = status
				return nil
			},
		},
		&mockRepoStore{},
		&mockGitHubClient{},
		&mockMailer{},
	)

	err := svc.Confirm(context.Background(), "valid-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updatedID != 42 {
		t.Errorf("updated ID = %d, want 42", updatedID)
	}
	if updatedStatus != model.StatusActive {
		t.Errorf("updated status = %q, want %q", updatedStatus, model.StatusActive)
	}
}

func TestConfirm_TokenNotFound(t *testing.T) {
	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*model.Subscription, error) {
				return nil, fmt.Errorf("subscription token: %w", repository.ErrNotFound)
			},
		},
		&mockRepoStore{},
		&mockGitHubClient{},
		&mockMailer{},
	)

	err := svc.Confirm(context.Background(), "invalid-token")
	if !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("got %v, want ErrTokenNotFound", err)
	}
}

func TestConfirm_AlreadyActive_Idempotent(t *testing.T) {
	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*model.Subscription, error) {
				return &model.Subscription{ID: 1, Status: model.StatusActive}, nil
			},
		},
		&mockRepoStore{},
		&mockGitHubClient{},
		&mockMailer{},
	)

	err := svc.Confirm(context.Background(), "token")
	if err != nil {
		t.Errorf("got %v, want nil (idempotent confirm)", err)
	}
}

func TestConfirm_UnsubscribedToken(t *testing.T) {
	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*model.Subscription, error) {
				return &model.Subscription{ID: 1, Status: model.StatusUnsubscribed}, nil
			},
		},
		&mockRepoStore{},
		&mockGitHubClient{},
		&mockMailer{},
	)

	err := svc.Confirm(context.Background(), "token")
	if !errors.Is(err, ErrSubscriptionInactive) {
		t.Errorf("got %v, want ErrSubscriptionInactive", err)
	}
}

func TestConfirm_DBError_Propagates(t *testing.T) {
	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*model.Subscription, error) {
				return nil, errors.New("connection refused")
			},
		},
		&mockRepoStore{},
		&mockGitHubClient{},
		&mockMailer{},
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
	var updatedStatus model.SubscriptionStatus

	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*model.Subscription, error) {
				return &model.Subscription{ID: 10, Status: model.StatusActive}, nil
			},
			UpdateStatusFn: func(_ context.Context, _ int64, status model.SubscriptionStatus) error {
				updatedStatus = status
				return nil
			},
		},
		&mockRepoStore{},
		&mockGitHubClient{},
		&mockMailer{},
	)

	err := svc.Unsubscribe(context.Background(), "valid-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updatedStatus != model.StatusUnsubscribed {
		t.Errorf("status = %q, want %q", updatedStatus, model.StatusUnsubscribed)
	}
}

func TestUnsubscribe_TokenNotFound(t *testing.T) {
	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*model.Subscription, error) {
				return nil, fmt.Errorf("subscription token: %w", repository.ErrNotFound)
			},
		},
		&mockRepoStore{},
		&mockGitHubClient{},
		&mockMailer{},
	)

	err := svc.Unsubscribe(context.Background(), "bad-token")
	if !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("got %v, want ErrTokenNotFound", err)
	}
}

func TestUnsubscribe_AlreadyUnsubscribed_Idempotent(t *testing.T) {
	updateCalled := false
	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*model.Subscription, error) {
				return &model.Subscription{ID: 10, Status: model.StatusUnsubscribed}, nil
			},
			UpdateStatusFn: func(_ context.Context, _ int64, _ model.SubscriptionStatus) error {
				updateCalled = true
				return nil
			},
		},
		&mockRepoStore{},
		&mockGitHubClient{},
		&mockMailer{},
	)

	err := svc.Unsubscribe(context.Background(), "token")
	if err != nil {
		t.Errorf("got %v, want nil (idempotent unsubscribe)", err)
	}
	if updateCalled {
		t.Error("should not call UpdateStatus for already-unsubscribed subscription")
	}
}

func TestUnsubscribe_DBError_Propagates(t *testing.T) {
	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{
			GetByTokenFn: func(_ context.Context, _ string) (*model.Subscription, error) {
				return nil, errors.New("connection refused")
			},
		},
		&mockRepoStore{},
		&mockGitHubClient{},
		&mockMailer{},
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
	expected := []model.Subscription{
		{ID: 1, Email: testEmail, RepoOwner: "golang", RepoName: "go", Status: model.StatusActive},
	}

	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{
			GetActiveByEmailFn: func(_ context.Context, email string) ([]model.Subscription, error) {
				if email != testEmail {
					t.Errorf("queried email = %q, want %q", email, testEmail)
				}
				return expected, nil
			},
		},
		&mockRepoStore{},
		&mockGitHubClient{},
		&mockMailer{},
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
	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{},
		&mockRepoStore{},
		&mockGitHubClient{},
		&mockMailer{},
	)

	_, err := svc.GetSubscriptions(context.Background(), "")
	if !errors.Is(err, ErrInvalidEmail) {
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

		svc := newTestSubscriptionService(
			&mockSubscriptionRepo{
				ExistsFn: func(_ context.Context, _, _, _ string) (bool, error) { return false, nil },
				CreateFn: func(_ context.Context, sub *model.Subscription) error {
					storedEmail = sub.Email
					return nil
				},
			},
			&mockRepoStore{
				UpsertFn: func(_ context.Context, _, _ string) (*model.TrackedRepository, error) {
					return &model.TrackedRepository{ID: 1}, nil
				},
			},
			&mockGitHubClient{
				RepoExistsFn: func(_ context.Context, _, _ string) (bool, error) { return true, nil },
			},
			&mockMailer{
				SendConfirmationFn: func(_ context.Context, _, _, _ string) error { return nil },
			},
		)

		err := svc.Subscribe(context.Background(), tt.input, "golang/go")
		if err != nil {
			t.Fatalf("Subscribe(%q): unexpected error: %v", tt.input, err)
		}
		if storedEmail != tt.want {
			t.Errorf("Subscribe(%q): stored email = %q, want %q", tt.input, storedEmail, tt.want)
		}
	}
}

func TestGetSubscriptions_NormalizesEmail(t *testing.T) {
	var queriedEmail string

	svc := newTestSubscriptionService(
		&mockSubscriptionRepo{
			GetActiveByEmailFn: func(_ context.Context, email string) ([]model.Subscription, error) {
				queriedEmail = email
				return []model.Subscription{}, nil
			},
		},
		&mockRepoStore{},
		&mockGitHubClient{},
		&mockMailer{},
	)

	_, err := svc.GetSubscriptions(context.Background(), "USER@Example.COM")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if queriedEmail != testEmail {
		t.Errorf("queried email = %q, want %q", queriedEmail, testEmail)
	}
}
