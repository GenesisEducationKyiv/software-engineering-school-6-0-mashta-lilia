// Package testgithub is an in-process programmable fake of the GitHub client for integration tests.
package testgithub

import (
	"context"
	"github-release-notifier/internal/release"
	"sync"
)

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

// Mirrors the real client so the interface stays satisfied; poller-only path, not HTTP.
func (f *Fake) GetLatestRelease(_ context.Context, _, _ string) (*release.Release, error) {
	return nil, nil
}
