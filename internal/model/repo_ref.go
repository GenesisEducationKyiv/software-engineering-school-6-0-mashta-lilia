package model

import (
	"errors"
	"strings"
)

// ErrInvalidRepoRef is returned when raw input does not match the
// "owner/name" GitHub repository format.
var ErrInvalidRepoRef = errors.New("invalid repository format, expected owner/repo")

// RepoRef is a validated GitHub "owner/name" reference. Constructing one
// guarantees both segments are non-empty and that name does not contain
// further slashes — so service/repository code can trust the structure
// without re-parsing.
type RepoRef struct {
	Owner string
	Name  string
}

// ParseRepoRef accepts the canonical "owner/name" form and rejects empty
// segments or extra slashes. The split logic lives here, not in the
// service layer, because RepoRef is the Information Expert for what a
// well-formed repo reference looks like.
func ParseRepoRef(raw string) (RepoRef, error) {
	owner, name, ok := strings.Cut(raw, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return RepoRef{}, ErrInvalidRepoRef
	}
	return RepoRef{Owner: owner, Name: name}, nil
}

// String returns the canonical "owner/name" form — useful for log lines
// and email subject construction.
func (r RepoRef) String() string {
	return r.Owner + "/" + r.Name
}
