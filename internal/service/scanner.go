package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type Scanner struct {
	repos    RepoStore
	subs     SubscriptionRepo
	github   GitHubClient
	mailer   Mailer
	interval time.Duration
	mu       sync.Mutex
	running  bool
}

func NewScanner(
	repos RepoStore, subs SubscriptionRepo, gh GitHubClient, mailer Mailer, interval time.Duration,
) *Scanner {
	return &Scanner{
		repos:    repos,
		subs:     subs,
		github:   gh,
		mailer:   mailer,
		interval: interval,
	}
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

func (s *Scanner) scan(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		slog.Info("scanner: previous scan still running, skipping")
		return
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	repos, err := s.repos.GetAll(ctx)
	if err != nil {
		slog.Error("scanner: failed to get repos", "error", err)
		return
	}

	for _, repo := range repos {
		if ctx.Err() != nil {
			return
		}

		release, err := s.github.GetLatestRelease(ctx, repo.Owner, repo.Name)
		if err != nil {
			slog.Error("scanner: failed to get release", "repo", repo.FullName(), "error", err)
			continue
		}
		if release == nil {
			continue
		}

		if repo.LastSeenTag.Valid && release.TagName == repo.LastSeenTag.String {
			// Tag unchanged — still record that we checked this repo so
			// last_checked_at stays current for staleness monitoring.
			if err := s.repos.UpdateLastChecked(ctx, repo.ID); err != nil {
				slog.Error("scanner: failed to update last_checked_at", "repo", repo.FullName(), "error", err)
			}
			continue
		}

		slog.Info("scanner: new release", "tag", release.TagName, "repo", repo.FullName())

		// Persist tag BEFORE notifying to prevent duplicate notifications on DB failure
		if err := s.repos.UpdateLastSeen(ctx, repo.ID, release.TagName); err != nil {
			slog.Error("scanner: failed to update last seen tag", "repo", repo.FullName(), "error", err)
			continue
		}

		emails, err := s.subs.GetEmailsByRepo(ctx, repo.Owner, repo.Name)
		if err != nil {
			slog.Error("scanner: failed to get subscribers", "repo", repo.FullName(), "error", err)
			continue
		}

		for _, email := range emails {
			if err := s.mailer.SendReleaseNotification(ctx, email, repo.FullName(), release); err != nil {
				slog.Error("scanner: failed to notify subscriber", "email", email, "error", err)
			}
		}
	}
}
