package release

import (
	"context"
	"errors"
	"fmt"
	"github-release-notifier/internal/repository"
	"log/slog"
	"sync"
	"time"
)

type repoScanReader interface {
	GetAll(ctx context.Context) ([]repository.Repository, error)
	UpdateLastSeen(ctx context.Context, id int64, tag string) error
	UpdateLastChecked(ctx context.Context, id int64) error
}

type subscriberLister interface {
	GetEmailsByRepo(ctx context.Context, owner, name string) ([]string, error)
}

type githubReleaseClient interface {
	GetLatestRelease(ctx context.Context, owner, name string) (*Release, error)
}

type releaseNotifier interface {
	SendReleaseNotification(ctx context.Context, email, repo string, r *Release) error
}

type Poller struct {
	repos    repoScanReader
	subs     subscriberLister
	github   githubReleaseClient
	mailer   releaseNotifier
	interval time.Duration
	log      *slog.Logger
	scanLock sync.Mutex
	done     chan struct{}
	doneOnce sync.Once
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
	if repos == nil {
		return nil, errors.New("poller: repos store is nil")
	}
	if subs == nil {
		return nil, errors.New("poller: subscriber store is nil")
	}
	if gh == nil {
		return nil, errors.New("poller: github client is nil")
	}
	if mailer == nil {
		return nil, errors.New("poller: mailer is nil")
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

func (p *Poller) Done() <-chan struct{} { return p.done }

func (p *Poller) Start(ctx context.Context) {
	defer p.doneOnce.Do(func() { close(p.done) })

	if ctx == nil {
		p.log.Error("poller start failed: nil context")
		return
	}

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

func (p *Poller) scanRepository(ctx context.Context, repo repository.Repository) {
	rel, err := p.github.GetLatestRelease(ctx, repo.Owner, repo.Name)
	if err != nil {
		p.log.Error("Failed to get release", "repo", repo.FullName(), "err", err)
		return
	}
	if rel == nil {
		p.updateLastChecked(ctx, repo)
		return
	}

	if repo.LastSeenTag.Valid && rel.TagName == repo.LastSeenTag.String {
		p.updateLastChecked(ctx, repo)
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

func (p *Poller) updateLastChecked(ctx context.Context, repo repository.Repository) {
	if err := p.repos.UpdateLastChecked(ctx, repo.ID); err != nil {
		p.log.Error("Failed to update last_checked_at", "repo", repo.FullName(), "err", err)
	}
}

func (p *Poller) notifySubscribers(ctx context.Context, repo repository.Repository, rel *Release) {
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
