//nolint:testpackage // white-box tests that use mocks from mocks_test.go
package service

import (
	"context"
	"database/sql"
	"errors"
	"github-release-notifier/internal/model"
	"io"
	"log"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	m.Run()
}

func mustNewScanner(
	t *testing.T,
	repos RepoStore, subs SubscriptionRepo, gh GitHubClient, mailer Mailer, interval time.Duration,
) *Scanner {
	t.Helper()
	s, err := NewScanner(repos, subs, gh, mailer, interval)
	if err != nil {
		t.Fatalf("NewScanner: %v", err)
	}
	return s
}

func TestNewScanner_RejectsNonPositiveInterval(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		if _, err := NewScanner(nil, nil, nil, nil, d); err == nil {
			t.Errorf("NewScanner(interval=%s) returned nil error, want non-nil", d)
		}
	}
}

func TestScanner_NewRelease_NotifiesSubscribers(t *testing.T) {
	var updatedTag string
	var notifiedEmails []string

	repos := &mockRepoStore{
		GetAllFn: func(_ context.Context) ([]model.TrackedRepository, error) {
			return []model.TrackedRepository{
				{ID: 1, Owner: "golang", Name: "go", LastSeenTag: sql.NullString{String: "v1.21", Valid: true}}, //nolint:revive // line exceeds limit due to test data
			}, nil
		},
		UpdateLastSeenFn: func(_ context.Context, _ int64, tag string) error {
			updatedTag = tag
			return nil
		},
	}

	subs := &mockSubscriptionRepo{
		GetEmailsByRepoFn: func(_ context.Context, _, _ string) ([]string, error) {
			return []string{"alice@example.com", "bob@example.com"}, nil
		},
	}

	gh := &mockGitHubClient{
		GetLatestReleaseFn: func(_ context.Context, _, _ string) (*model.Release, error) {
			return &model.Release{
				TagName: "v1.22", Name: "Go 1.22",
				HTMLURL: "https://github.com/golang/go/releases/tag/v1.22",
			}, nil
		},
	}

	mailer := &mockMailer{
		SendReleaseNotificationFn: func(_ context.Context, email, _ string, _ *model.Release) error {
			notifiedEmails = append(notifiedEmails, email)
			return nil
		},
	}

	scanner := mustNewScanner(t,repos, subs, gh, mailer, time.Hour)

	scanner.scan(context.Background())

	if updatedTag != "v1.22" {
		t.Errorf("updated tag = %q, want %q", updatedTag, "v1.22")
	}
	if len(notifiedEmails) != 2 {
		t.Fatalf("notified %d emails, want 2", len(notifiedEmails))
	}
	if notifiedEmails[0] != "alice@example.com" || notifiedEmails[1] != "bob@example.com" {
		t.Errorf("notified = %v, want [alice@example.com, bob@example.com]", notifiedEmails)
	}
}

func TestScanner_SameTag_NoNotification(t *testing.T) {
	notifyCalled := false

	repos := &mockRepoStore{
		GetAllFn: func(_ context.Context) ([]model.TrackedRepository, error) {
			return []model.TrackedRepository{
				{ID: 1, Owner: "golang", Name: "go", LastSeenTag: sql.NullString{String: "v1.22", Valid: true}}, //nolint:revive // line exceeds limit due to test data
			}, nil
		},
	}

	gh := &mockGitHubClient{
		GetLatestReleaseFn: func(_ context.Context, _, _ string) (*model.Release, error) {
			return &model.Release{TagName: "v1.22"}, nil
		},
	}

	mailer := &mockMailer{
		SendReleaseNotificationFn: func(_ context.Context, _, _ string, _ *model.Release) error {
			notifyCalled = true
			return nil
		},
	}

	scanner := mustNewScanner(t,repos, &mockSubscriptionRepo{}, gh, mailer, time.Hour)

	scanner.scan(context.Background())

	if notifyCalled {
		t.Error("should not notify when tag hasn't changed")
	}
}

func TestScanner_NullLastSeenTag_TreatsAsNew(t *testing.T) {
	var updatedTag string

	repos := &mockRepoStore{
		GetAllFn: func(_ context.Context) ([]model.TrackedRepository, error) {
			return []model.TrackedRepository{
				{ID: 1, Owner: "golang", Name: "go", LastSeenTag: sql.NullString{Valid: false}},
			}, nil
		},
		UpdateLastSeenFn: func(_ context.Context, _ int64, tag string) error {
			updatedTag = tag
			return nil
		},
	}

	subs := &mockSubscriptionRepo{
		GetEmailsByRepoFn: func(_ context.Context, _, _ string) ([]string, error) {
			return []string{}, nil
		},
	}

	gh := &mockGitHubClient{
		GetLatestReleaseFn: func(_ context.Context, _, _ string) (*model.Release, error) {
			return &model.Release{TagName: "v1.0.0"}, nil
		},
	}

	mailer := &mockMailer{
		SendReleaseNotificationFn: func(_ context.Context, _, _ string, _ *model.Release) error { return nil }, //nolint:revive // line exceeds limit due to test data
	}

	scanner := mustNewScanner(t,repos, subs, gh, mailer, time.Hour)

	scanner.scan(context.Background())

	if updatedTag != "v1.0.0" {
		t.Errorf("updated tag = %q, want %q", updatedTag, "v1.0.0")
	}
}

func TestScanner_NoRelease_Skips(t *testing.T) {
	updateCalled := false

	repos := &mockRepoStore{
		GetAllFn: func(_ context.Context) ([]model.TrackedRepository, error) {
			return []model.TrackedRepository{
				{ID: 1, Owner: "new", Name: "repo"},
			}, nil
		},
		UpdateLastSeenFn: func(_ context.Context, _ int64, _ string) error {
			updateCalled = true
			return nil
		},
	}

	gh := &mockGitHubClient{
		GetLatestReleaseFn: func(_ context.Context, _, _ string) (*model.Release, error) {
			return nil, nil // no releases
		},
	}

	scanner := mustNewScanner(t,repos, &mockSubscriptionRepo{}, gh, &mockMailer{}, time.Hour)

	scanner.scan(context.Background())

	if updateCalled {
		t.Error("should not update tag when no release exists")
	}
}

func TestScanner_GitHubError_ContinuesOtherRepos(t *testing.T) {
	var updatedTags []string

	repos := &mockRepoStore{
		GetAllFn: func(_ context.Context) ([]model.TrackedRepository, error) {
			return []model.TrackedRepository{
				{ID: 1, Owner: "fail", Name: "repo"},
				{ID: 2, Owner: "ok", Name: "repo", LastSeenTag: sql.NullString{String: "v1.0", Valid: true}},
			}, nil
		},
		UpdateLastSeenFn: func(_ context.Context, _ int64, tag string) error {
			updatedTags = append(updatedTags, tag)
			return nil
		},
	}

	subs := &mockSubscriptionRepo{
		GetEmailsByRepoFn: func(_ context.Context, _, _ string) ([]string, error) {
			return []string{}, nil
		},
	}

	gh := &mockGitHubClient{
		GetLatestReleaseFn: func(_ context.Context, owner, _ string) (*model.Release, error) {
			if owner == "fail" {
				return nil, errors.New("rate limited")
			}
			return &model.Release{TagName: "v2.0"}, nil
		},
	}

	mailer := &mockMailer{
		SendReleaseNotificationFn: func(_ context.Context, _, _ string, _ *model.Release) error { return nil }, //nolint:revive // line exceeds limit due to test data
	}

	scanner := mustNewScanner(t,repos, subs, gh, mailer, time.Hour)

	scanner.scan(context.Background())

	if len(updatedTags) != 1 || updatedTags[0] != "v2.0" {
		t.Errorf("updated tags = %v, want [v2.0]", updatedTags)
	}
}

func TestScanner_UpdateLastSeenFails_SkipsNotification(t *testing.T) {
	notifyCalled := false

	repos := &mockRepoStore{
		GetAllFn: func(_ context.Context) ([]model.TrackedRepository, error) {
			return []model.TrackedRepository{
				{ID: 1, Owner: "golang", Name: "go", LastSeenTag: sql.NullString{String: "v1.0", Valid: true}}, //nolint:revive // line exceeds limit due to test data
			}, nil
		},
		UpdateLastSeenFn: func(_ context.Context, _ int64, _ string) error {
			return errors.New("db error")
		},
	}

	gh := &mockGitHubClient{
		GetLatestReleaseFn: func(_ context.Context, _, _ string) (*model.Release, error) {
			return &model.Release{TagName: "v2.0"}, nil
		},
	}

	mailer := &mockMailer{
		SendReleaseNotificationFn: func(_ context.Context, _, _ string, _ *model.Release) error {
			notifyCalled = true
			return nil
		},
	}

	scanner := mustNewScanner(t,repos, &mockSubscriptionRepo{}, gh, mailer, time.Hour)

	scanner.scan(context.Background())

	if notifyCalled {
		t.Error("should NOT send notifications when UpdateLastSeen fails (persist-before-notify)")
	}
}

func TestScanner_ContextCancelled_StopsProcessing(t *testing.T) {
	processedCount := 0

	repos := &mockRepoStore{
		GetAllFn: func(_ context.Context) ([]model.TrackedRepository, error) {
			return []model.TrackedRepository{
				{ID: 1, Owner: "a", Name: "repo"},
				{ID: 2, Owner: "b", Name: "repo"},
				{ID: 3, Owner: "c", Name: "repo"},
			}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	gh := &mockGitHubClient{
		GetLatestReleaseFn: func(_ context.Context, owner, _ string) (*model.Release, error) {
			processedCount++
			if owner == "a" {
				cancel() // cancel after first repo
			}
			return nil, nil
		},
	}

	scanner := mustNewScanner(t,repos, &mockSubscriptionRepo{}, gh, &mockMailer{}, time.Hour)

	scanner.scan(ctx)

	if processedCount > 2 {
		t.Errorf("processed %d repos after cancel, expected early exit", processedCount)
	}
}
