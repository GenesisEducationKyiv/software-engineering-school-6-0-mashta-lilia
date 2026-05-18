//nolint:testpackage // white-box tests share unexported helpers
package release

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.Run()
}

func mustNewPoller(
	t *testing.T,
	repos *mockRepoScanReader, subs *mockSubscriberLister,
	gh *mockGitHubReleaseClient, mailer *mockReleaseNotifier, interval time.Duration,
) *Poller {
	t.Helper()
	p, err := NewPoller(repos, subs, gh, mailer, interval)
	if err != nil {
		t.Fatalf("NewPoller: %v", err)
	}
	return p
}

func TestNewPoller_RejectsNonPositiveInterval(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		if _, err := NewPoller(nil, nil, nil, nil, d); err == nil {
			t.Errorf("NewPoller(interval=%s) returned nil error, want non-nil", d)
		}
	}
}

func TestPoller_NewRelease_NotifiesSubscribers(t *testing.T) {
	var updatedTag string
	var notifiedEmails []string

	repos := &mockRepoScanReader{
		GetAllFn: func(_ context.Context) ([]TrackedRepository, error) {
			return []TrackedRepository{
				{ID: 1, Owner: "golang", Name: "go", LastSeenTag: sql.NullString{String: "v1.21", Valid: true}},
			}, nil
		},
		UpdateLastSeenFn: func(_ context.Context, _ int64, tag string) error {
			updatedTag = tag
			return nil
		},
	}
	subs := &mockSubscriberLister{
		GetEmailsByRepoFn: func(_ context.Context, _, _ string) ([]string, error) {
			return []string{"alice@example.com", "bob@example.com"}, nil
		},
	}
	gh := &mockGitHubReleaseClient{
		GetLatestReleaseFn: func(_ context.Context, _, _ string) (*Release, error) {
			return &Release{
				TagName: "v1.22", Name: "Go 1.22",
				HTMLURL: "https://github.com/golang/go/releases/tag/v1.22",
			}, nil
		},
	}
	mailer := &mockReleaseNotifier{
		SendReleaseNotificationFn: func(_ context.Context, email, _ string, _ *Release) error {
			notifiedEmails = append(notifiedEmails, email)
			return nil
		},
	}

	poller := mustNewPoller(t, repos, subs, gh, mailer, time.Hour)
	poller.scan(context.Background())

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

func TestPoller_SameTag_NoNotification(t *testing.T) {
	notifyCalled := false
	var checkedID int64

	repos := &mockRepoScanReader{
		GetAllFn: func(_ context.Context) ([]TrackedRepository, error) {
			return []TrackedRepository{
				{ID: 1, Owner: "golang", Name: "go", LastSeenTag: sql.NullString{String: "v1.22", Valid: true}},
			}, nil
		},
		UpdateLastCheckedFn: func(_ context.Context, id int64) error {
			checkedID = id
			return nil
		},
	}
	gh := &mockGitHubReleaseClient{
		GetLatestReleaseFn: func(_ context.Context, _, _ string) (*Release, error) {
			return &Release{TagName: "v1.22"}, nil
		},
	}
	mailer := &mockReleaseNotifier{
		SendReleaseNotificationFn: func(_ context.Context, _, _ string, _ *Release) error {
			notifyCalled = true
			return nil
		},
	}

	poller := mustNewPoller(t, repos, &mockSubscriberLister{}, gh, mailer, time.Hour)
	poller.scan(context.Background())

	if notifyCalled {
		t.Error("should not notify when tag hasn't changed")
	}
	if checkedID != 1 {
		t.Errorf("checked ID = %d, want 1", checkedID)
	}
}

func TestPoller_NullLastSeenTag_TreatsAsNew(t *testing.T) {
	var updatedTag string

	repos := &mockRepoScanReader{
		GetAllFn: func(_ context.Context) ([]TrackedRepository, error) {
			return []TrackedRepository{
				{ID: 1, Owner: "golang", Name: "go", LastSeenTag: sql.NullString{Valid: false}},
			}, nil
		},
		UpdateLastSeenFn: func(_ context.Context, _ int64, tag string) error {
			updatedTag = tag
			return nil
		},
	}
	subs := &mockSubscriberLister{
		GetEmailsByRepoFn: func(_ context.Context, _, _ string) ([]string, error) {
			return []string{}, nil
		},
	}
	gh := &mockGitHubReleaseClient{
		GetLatestReleaseFn: func(_ context.Context, _, _ string) (*Release, error) {
			return &Release{TagName: "v1.0.0"}, nil
		},
	}
	mailer := &mockReleaseNotifier{
		SendReleaseNotificationFn: func(_ context.Context, _, _ string, _ *Release) error { return nil },
	}

	poller := mustNewPoller(t, repos, subs, gh, mailer, time.Hour)
	poller.scan(context.Background())

	if updatedTag != "v1.0.0" {
		t.Errorf("updated tag = %q, want %q", updatedTag, "v1.0.0")
	}
}

func TestPoller_NoRelease_Skips(t *testing.T) {
	updateCalled := false
	checkedCalled := false

	repos := &mockRepoScanReader{
		GetAllFn: func(_ context.Context) ([]TrackedRepository, error) {
			return []TrackedRepository{{ID: 1, Owner: "new", Name: "repo"}}, nil
		},
		UpdateLastSeenFn: func(_ context.Context, _ int64, _ string) error {
			updateCalled = true
			return nil
		},
		UpdateLastCheckedFn: func(_ context.Context, id int64) error {
			if id != 1 {
				t.Errorf("checked ID = %d, want 1", id)
			}
			checkedCalled = true
			return nil
		},
	}
	gh := &mockGitHubReleaseClient{
		GetLatestReleaseFn: func(_ context.Context, _, _ string) (*Release, error) {
			return nil, nil
		},
	}

	poller := mustNewPoller(t, repos, &mockSubscriberLister{}, gh, &mockReleaseNotifier{}, time.Hour)
	poller.scan(context.Background())

	if updateCalled {
		t.Error("should not update tag when no release exists")
	}
	if !checkedCalled {
		t.Error("should update last_checked_at when no release exists")
	}
}

func TestPoller_GitHubError_ContinuesOtherRepos(t *testing.T) {
	var updatedTags []string

	repos := &mockRepoScanReader{
		GetAllFn: func(_ context.Context) ([]TrackedRepository, error) {
			return []TrackedRepository{
				{ID: 1, Owner: "fail", Name: "repo"},
				{ID: 2, Owner: "ok", Name: "repo", LastSeenTag: sql.NullString{String: "v1.0", Valid: true}},
			}, nil
		},
		UpdateLastSeenFn: func(_ context.Context, _ int64, tag string) error {
			updatedTags = append(updatedTags, tag)
			return nil
		},
	}
	subs := &mockSubscriberLister{
		GetEmailsByRepoFn: func(_ context.Context, _, _ string) ([]string, error) {
			return []string{}, nil
		},
	}
	gh := &mockGitHubReleaseClient{
		GetLatestReleaseFn: func(_ context.Context, owner, _ string) (*Release, error) {
			if owner == "fail" {
				return nil, errors.New("rate limited")
			}
			return &Release{TagName: "v2.0"}, nil
		},
	}
	mailer := &mockReleaseNotifier{
		SendReleaseNotificationFn: func(_ context.Context, _, _ string, _ *Release) error { return nil },
	}

	poller := mustNewPoller(t, repos, subs, gh, mailer, time.Hour)
	poller.scan(context.Background())

	if len(updatedTags) != 1 || updatedTags[0] != "v2.0" {
		t.Errorf("updated tags = %v, want [v2.0]", updatedTags)
	}
}

func TestPoller_UpdateLastSeenFails_SkipsNotification(t *testing.T) {
	notifyCalled := false

	repos := &mockRepoScanReader{
		GetAllFn: func(_ context.Context) ([]TrackedRepository, error) {
			return []TrackedRepository{
				{ID: 1, Owner: "golang", Name: "go", LastSeenTag: sql.NullString{String: "v1.0", Valid: true}},
			}, nil
		},
		UpdateLastSeenFn: func(_ context.Context, _ int64, _ string) error {
			return errors.New("db error")
		},
	}
	gh := &mockGitHubReleaseClient{
		GetLatestReleaseFn: func(_ context.Context, _, _ string) (*Release, error) {
			return &Release{TagName: "v2.0"}, nil
		},
	}
	mailer := &mockReleaseNotifier{
		SendReleaseNotificationFn: func(_ context.Context, _, _ string, _ *Release) error {
			notifyCalled = true
			return nil
		},
	}

	poller := mustNewPoller(t, repos, &mockSubscriberLister{}, gh, mailer, time.Hour)
	poller.scan(context.Background())

	if notifyCalled {
		t.Error("should NOT send notifications when UpdateLastSeen fails (persist-before-notify)")
	}
}

func TestPoller_ContextCancelled_StopsProcessing(t *testing.T) {
	processedCount := 0

	repos := &mockRepoScanReader{
		GetAllFn: func(_ context.Context) ([]TrackedRepository, error) {
			return []TrackedRepository{
				{ID: 1, Owner: "a", Name: "repo"},
				{ID: 2, Owner: "b", Name: "repo"},
				{ID: 3, Owner: "c", Name: "repo"},
			}, nil
		},
		UpdateLastCheckedFn: func(_ context.Context, _ int64) error { return nil },
	}

	ctx, cancel := context.WithCancel(context.Background())

	gh := &mockGitHubReleaseClient{
		GetLatestReleaseFn: func(_ context.Context, owner, _ string) (*Release, error) {
			processedCount++
			if owner == "a" {
				cancel()
			}
			return nil, nil
		},
	}

	poller := mustNewPoller(t, repos, &mockSubscriberLister{}, gh, &mockReleaseNotifier{}, time.Hour)
	poller.scan(ctx)

	if processedCount > 2 {
		t.Errorf("processed %d repos after cancel, expected early exit", processedCount)
	}
}

func TestPoller_OverlappingScanSkipped(t *testing.T) {
	firstScanStarted := make(chan struct{})
	releaseFirstScan := make(chan struct{})
	firstScanDone := make(chan struct{})
	secondScanDone := make(chan struct{})
	var getAllCalls atomic.Int32

	repos := &mockRepoScanReader{
		GetAllFn: func(_ context.Context) ([]TrackedRepository, error) {
			if getAllCalls.Add(1) == 1 {
				close(firstScanStarted)
				<-releaseFirstScan
			}
			return nil, nil
		},
	}
	poller := mustNewPoller(
		t, repos, &mockSubscriberLister{}, &mockGitHubReleaseClient{}, &mockReleaseNotifier{}, time.Hour,
	)

	go func() {
		defer close(firstScanDone)
		poller.scan(context.Background())
	}()

	<-firstScanStarted
	go func() {
		defer close(secondScanDone)
		poller.scan(context.Background())
	}()

	select {
	case <-secondScanDone:
	case <-time.After(100 * time.Millisecond):
		close(releaseFirstScan)
		<-firstScanDone
		t.Fatal("overlapping scan did not return promptly")
	}

	close(releaseFirstScan)
	<-firstScanDone

	if got := getAllCalls.Load(); got != 1 {
		t.Errorf("GetAll calls = %d, want 1", got)
	}
}
