package testapp

import (
	"context"
	"sync"

	"github-release-notifier/internal/release"
)

// FakeGithub satisfies the subscription package's `githubChecker` interface
// purely in-process. Programmable per-test via SetRepoExists / SetError so
// integration tests don't hit the real GitHub API (that's the GitHub
// client's own unit-test surface — see internal/client/github/*_test.go).
type FakeGithub struct {
	mu     sync.Mutex
	exists map[string]bool
	err    error
}

func NewFakeGithub() *FakeGithub {
	return &FakeGithub{exists: map[string]bool{}}
}

func (f *FakeGithub) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exists = map[string]bool{}
	f.err = nil
}

func (f *FakeGithub) SetRepoExists(owner, name string, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exists[owner+"/"+name] = ok
}

func (f *FakeGithub) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *FakeGithub) RepoExists(_ context.Context, owner, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	return f.exists[owner+"/"+name], nil
}

// GetLatestRelease is not exercised through HTTP endpoints (the release
// poller uses it), but the interface mirrors the real client so we satisfy
// both contracts and stay forward-compatible.
func (f *FakeGithub) GetLatestRelease(_ context.Context, _, _ string) (*release.Release, error) {
	return nil, nil
}
