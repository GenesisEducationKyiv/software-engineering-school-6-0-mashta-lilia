package service

import (
	"context"
	"fmt"
	"github-release-notifier/internal/model"
	"log/slog"
	"sync"
	"time"
)

// Scanner depends on the narrowest set of capabilities it actually needs:
// it reads/updates tracked repos via RepoScanReader, lists subscribers via
// SubscriberLister, and sends release notifications via ReleaseNotifier.
// It has no access to subscription writes, repo upserts, or confirmation
// emails — those are SubscriptionService's concern.
type Scanner struct {
	repos    RepoScanReader
	subs     SubscriberLister
	github   GitHubClient
	mailer   ReleaseNotifier
	interval time.Duration
	mu       sync.Mutex
	running  bool
}

func NewScanner(
	repos RepoScanReader,
	subs SubscriberLister,
	gh GitHubClient,
	mailer ReleaseNotifier,
	interval time.Duration,
) (*Scanner, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("scanner interval must be > 0, got %s", interval)
	}
	return &Scanner{
		repos:    repos,
		subs:     subs,
		github:   gh,
		mailer:   mailer,
		interval: interval,
	}, nil
}

func (s *Scanner) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	slog.Info("scanner started", "interval", s.interval)

	s.scan(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("scanner stopped")
			return
		case <-ticker.C:
			s.scan(ctx)
		}
	}
}

// scan is the top-level scan entry point. It enforces single-flight semantics
// (skipping if a previous scan is still running) and delegates the actual
// per-repo work to scanRepository.
func (s *Scanner) scan(ctx context.Context) {
	if !s.acquireRunSlot() {
		slog.Info("scanner: previous scan still running, skipping")
		return
	}
	defer s.releaseRunSlot()

	repos, err := s.repos.GetAll(ctx)
	if err != nil {
		slog.Error("scanner: failed to get repos", "error", err)
		return
	}

	for _, repo := range repos {
		if ctx.Err() != nil {
			return
		}
		s.scanRepository(ctx, repo)
	}
}

// acquireRunSlot returns true and marks the scanner as running if no scan is
// currently in flight. Otherwise it returns false without changing state.
func (s *Scanner) acquireRunSlot() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	return true
}

func (s *Scanner) releaseRunSlot() {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
}

// scanRepository fetches the latest release for a single repo and, if a new
// tag is detected, persists it and notifies the subscribers.
func (s *Scanner) scanRepository(ctx context.Context, repo model.TrackedRepository) {
	release, err := s.github.GetLatestRelease(ctx, repo.Owner, repo.Name)
	if err != nil {
		slog.Error("scanner: failed to get release", "repo", repo.FullName(), "error", err)
		return
	}
	if release == nil {
		return
	}

	if repo.LastSeenTag.Valid && release.TagName == repo.LastSeenTag.String {
		// Tag unchanged — still record that we checked this repo so
		// last_checked_at stays current for staleness monitoring.
		if err := s.repos.UpdateLastChecked(ctx, repo.ID); err != nil {
			slog.Error("scanner: failed to update last_checked_at", "repo", repo.FullName(), "error", err)
		}
		return
	}

	slog.Info("scanner: new release", "tag", release.TagName, "repo", repo.FullName())

	// Persist tag BEFORE notifying to prevent duplicate notifications on DB failure.
	if err := s.repos.UpdateLastSeen(ctx, repo.ID, release.TagName); err != nil {
		slog.Error("scanner: failed to update last seen tag", "repo", repo.FullName(), "error", err)
		return
	}

	s.notifySubscribers(ctx, repo, release)
}

// notifySubscribers fetches the active subscribers for a repo and sends each
// one a release notification. Per-email failures are logged but do not abort
// the loop, so a transient SMTP issue for one user does not block the rest.
func (s *Scanner) notifySubscribers(
	ctx context.Context, repo model.TrackedRepository, release *model.Release,
) {
	emails, err := s.subs.GetEmailsByRepo(ctx, repo.Owner, repo.Name)
	if err != nil {
		slog.Error("scanner: failed to get subscribers", "repo", repo.FullName(), "error", err)
		return
	}

	for _, email := range emails {
		if err := s.mailer.SendReleaseNotification(ctx, email, repo.FullName(), release); err != nil {
			slog.Error("scanner: failed to notify subscriber", "repo", repo.FullName(), "error", err)
		}
	}
}
