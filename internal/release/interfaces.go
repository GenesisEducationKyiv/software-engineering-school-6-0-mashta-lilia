package release

import "context"

type repoScanReader interface {
	GetAll(ctx context.Context) ([]TrackedRepository, error)
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
