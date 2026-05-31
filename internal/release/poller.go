package release

import (
	"context"
	"errors"
	"fmt"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/platform/tracectx"
	"github-release-notifier/internal/repository"
	"sync"
	"time"

	"github.com/google/uuid"
)

const notifyWorkers = 8

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
	log      logger.Logger
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
	log logger.Logger,
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
	if log == nil {
		log = logger.Nop()
	}
	return &Poller{
		repos:    repos,
		subs:     subs,
		github:   gh,
		mailer:   mailer,
		interval: interval,
		log:      log,
		done:     make(chan struct{}),
	}, nil
}

func (p *Poller) Done() <-chan struct{} { return p.done }

func (p *Poller) Start(ctx context.Context) {
	defer p.doneOnce.Do(func() { close(p.done) })

	if ctx == nil {
		p.log.Error(context.Background(), "poller_start_failed", "err", errors.New("nil context"))
		return
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.log.Info(ctx, "poller_started", "interval", p.interval)

	p.scan(ctx)

	for {
		select {
		case <-ctx.Done():
			p.log.Info(ctx, "poller_stopped")
			return
		case <-ticker.C:
			p.scan(ctx)
		}
	}
}

func (p *Poller) scan(parentCtx context.Context) {
	if !p.scanLock.TryLock() {
		p.log.Info(parentCtx, "poller_scan_skipped", "reason", "previous_scan_running")
		return
	}
	defer p.scanLock.Unlock()

	ctx := tracectx.WithTraceID(parentCtx, uuid.NewString())

	repos, err := p.repos.GetAll(ctx)
	if err != nil {
		p.log.Error(ctx, "poller_get_repos_failed", "err", err)
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
		p.log.Error(ctx, "poller_get_release_failed", "repo", repo.FullName(), "err", err)
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

	p.log.Info(ctx, "new_release_detected", "tag", rel.TagName, "repo", repo.FullName())

	if err := p.repos.UpdateLastSeen(ctx, repo.ID, rel.TagName); err != nil {
		p.log.Error(ctx, "poller_update_last_seen_failed", "repo", repo.FullName(), "err", err)
		return
	}

	p.notifySubscribers(ctx, repo, rel)
}

func (p *Poller) updateLastChecked(ctx context.Context, repo repository.Repository) {
	if err := p.repos.UpdateLastChecked(ctx, repo.ID); err != nil {
		p.log.Error(ctx, "poller_update_last_checked_failed", "repo", repo.FullName(), "err", err)
	}
}

func (p *Poller) notifySubscribers(ctx context.Context, repo repository.Repository, rel *Release) {
	emails, err := p.subs.GetEmailsByRepo(ctx, repo.Owner, repo.Name)
	if err != nil {
		p.log.Error(ctx, "poller_get_subscribers_failed", "repo", repo.FullName(), "err", err)
		return
	}

	workers := notifyWorkers
	if len(emails) < workers {
		workers = len(emails)
	}
	if workers <= 1 {
		for _, email := range emails {
			p.sendOne(ctx, repo, rel, email)
		}
		return
	}

	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for email := range jobs {
				p.sendOne(ctx, repo, rel, email)
			}
		}()
	}
	for _, email := range emails {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		case jobs <- email:
		}
	}
	close(jobs)
	wg.Wait()
}

func (p *Poller) sendOne(ctx context.Context, repo repository.Repository, rel *Release, email string) {
	if err := p.mailer.SendReleaseNotification(ctx, email, repo.FullName(), rel); err != nil {
		p.log.Error(ctx, "poller_notify_subscriber_failed", "repo", repo.FullName(), "err", err)
	}
}
