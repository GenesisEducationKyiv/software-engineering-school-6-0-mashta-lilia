//nolint:testpackage // white-box tests use unexported interfaces
package release

import "context"

type mockRepoScanReader struct {
	GetAllFn            func(ctx context.Context) ([]TrackedRepository, error)
	UpdateLastSeenFn    func(ctx context.Context, id int64, tag string) error
	UpdateLastCheckedFn func(ctx context.Context, id int64) error
}

func (m *mockRepoScanReader) GetAll(ctx context.Context) ([]TrackedRepository, error) {
	if m.GetAllFn == nil {
		panic("mockRepoScanReader.GetAll called but not configured")
	}
	return m.GetAllFn(ctx)
}

func (m *mockRepoScanReader) UpdateLastSeen(ctx context.Context, id int64, tag string) error {
	if m.UpdateLastSeenFn == nil {
		panic("mockRepoScanReader.UpdateLastSeen called but not configured")
	}
	return m.UpdateLastSeenFn(ctx, id, tag)
}

func (m *mockRepoScanReader) UpdateLastChecked(ctx context.Context, id int64) error {
	if m.UpdateLastCheckedFn == nil {
		panic("mockRepoScanReader.UpdateLastChecked called but not configured")
	}
	return m.UpdateLastCheckedFn(ctx, id)
}

type mockSubscriberLister struct {
	GetEmailsByRepoFn func(ctx context.Context, owner, name string) ([]string, error)
}

func (m *mockSubscriberLister) GetEmailsByRepo(ctx context.Context, owner, name string) ([]string, error) {
	if m.GetEmailsByRepoFn == nil {
		panic("mockSubscriberLister.GetEmailsByRepo called but not configured")
	}
	return m.GetEmailsByRepoFn(ctx, owner, name)
}

type mockGitHubReleaseClient struct {
	GetLatestReleaseFn func(ctx context.Context, owner, name string) (*Release, error)
}

func (m *mockGitHubReleaseClient) GetLatestRelease(ctx context.Context, owner, name string) (*Release, error) {
	if m.GetLatestReleaseFn == nil {
		panic("mockGitHubReleaseClient.GetLatestRelease called but not configured")
	}
	return m.GetLatestReleaseFn(ctx, owner, name)
}

type mockReleaseNotifier struct {
	SendReleaseNotificationFn func(ctx context.Context, email, repo string, r *Release) error
}

func (m *mockReleaseNotifier) SendReleaseNotification(
	ctx context.Context, email, repo string, r *Release,
) error {
	if m.SendReleaseNotificationFn == nil {
		panic("mockReleaseNotifier.SendReleaseNotification called but not configured")
	}
	return m.SendReleaseNotificationFn(ctx, email, repo, r)
}
