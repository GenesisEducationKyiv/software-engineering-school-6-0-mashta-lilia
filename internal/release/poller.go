package release

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type Poller struct {
	repos    repoScanReader
	subs     subscriberLister
	github   githubReleaseClient
	mailer   releaseNotifier
	interval time.Duration
	log      *slog.Logger
	scanLock sync.Mutex
	done     chan struct{}
}

func NewPoller(
	repos repoScanReader,
	subs subscriberLister,
	gh githubReleaseClient,
	mailer releaseNotifier,
	interval time.Duration,
) (*Poller, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("poller interval must be > 0, got %s", interval)
	}
	return &Poller{
		repos:    repos,
		subs:     subs,
		github:   gh,
		mailer:   mailer,
		interval: interval,
		log:      slog.With("component", "poller"),
		done:     make(chan struct{}),
	}, nil
}

// Done returns a channel that is closed when Start has returned. Callers
// use this to wait for the poller to finish in-flight work during shutdown.
func (p *Poller) Done() <-chan struct{} { return p.done }

func (p *Poller) Start(ctx context.Context) {
	defer close(p.done)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.log.Info("Poller started", "interval", p.interval)

	p.scan(ctx)

	for {
		select {
		case <-ctx.Done():
			p.log.Info("Poller stopped")
			return
		case <-ticker.C:
			p.scan(ctx)
		}
	}
}

func (p *Poller) scan(ctx context.Context) {
	if !p.scanLock.TryLock() {
		p.log.Info("Previous scan still running, skipping")
		return
	}
	defer p.scanLock.Unlock()

	repos, err := p.repos.GetAll(ctx)
	if err != nil {
		p.log.Error("Failed to get repos", "err", err)
		return
	}

	for _, repo := range repos {
		if ctx.Err() != nil {
			return
		}
		p.scanRepository(ctx, repo)
	}
}

func (p *Poller) scanRepository(ctx context.Context, repo TrackedRepository) {
	rel, err := p.github.GetLatestRelease(ctx, repo.Owner, repo.Name)
	if err != nil {
		p.log.Error("Failed to get release", "repo", repo.FullName(), "err", err)
		return
	}
	if rel == nil {
		return
	}

	if repo.LastSeenTag.Valid && rel.TagName == repo.LastSeenTag.String {
		if err := p.repos.UpdateLastChecked(ctx, repo.ID); err != nil {
			p.log.Error("Failed to update last_checked_at", "repo", repo.FullName(), "err", err)
		}
		return
	}

	p.log.Info("New release detected", "tag", rel.TagName, "repo", repo.FullName())

	// Persist tag before notifying — see ADR-0007 (at-most-once).
	if err := p.repos.UpdateLastSeen(ctx, repo.ID, rel.TagName); err != nil {
		p.log.Error("Failed to update last seen tag", "repo", repo.FullName(), "err", err)
		return
	}

	p.notifySubscribers(ctx, repo, rel)
}

func (p *Poller) notifySubscribers(ctx context.Context, repo TrackedRepository, rel *Release) {
	emails, err := p.subs.GetEmailsByRepo(ctx, repo.Owner, repo.Name)
	if err != nil {
		p.log.Error("Failed to get subscribers", "repo", repo.FullName(), "err", err)
		return
	}

	for _, email := range emails {
		if err := p.mailer.SendReleaseNotification(ctx, email, repo.FullName(), rel); err != nil {
			p.log.Error("Failed to notify subscriber", "repo", repo.FullName(), "err", err)
		}
	}
}
