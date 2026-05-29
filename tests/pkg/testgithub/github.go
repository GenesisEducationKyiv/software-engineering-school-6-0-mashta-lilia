// Package testgithub provides an in-process programmable fake of the
// GitHub client used by integration tests. Tests configure repo
// existence / errors per case so they don't touch the real GitHub API
// (that's covered by internal/client/github/*_test.go).
package testgithub

import (
	"context"
	"github-release-notifier/internal/release"
	"sync"
)

// Fake satisfies the subscription package's `githubChecker` interface
// purely in-process. Programmable per-test via SetRepoExists / SetError.
type Fake struct {
	mu     sync.Mutex
	exists map[string]bool
	err    error
}

func New() *Fake {
	return &Fake{exists: map[string]bool{}}
}

func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exists = map[string]bool{}
	f.err = nil
}

func (f *Fake) SetRepoExists(owner, name string, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exists[owner+"/"+name] = ok
}

func (f *Fake) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *Fake) RepoExists(_ context.Context, owner, name string) (bool, error) {
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
func (f *Fake) GetLatestRelease(_ context.Context, _, _ string) (*release.Release, error) {
	return nil, nil
}
