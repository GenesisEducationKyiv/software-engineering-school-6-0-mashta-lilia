package repository

import (
	"errors"
	"regexp"
	"strings"
)

var ErrInvalid = errors.New("invalid repository format, expected owner/repo")

var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

// Rejects empty, leading/trailing -, and consecutive -- by construction; len cap in validOwner.
var ownerRe = regexp.MustCompile(`^[A-Za-z0-9](?:-?[A-Za-z0-9])*$`)

type Ref struct {
	Owner string
	Name  string
}

// Strict charset blocks CR/LF so the value is safe to interpolate into logs/URLs/headers.
func ParseRef(raw string) (Ref, error) {
	owner, name, ok := strings.Cut(raw, "/")
	if !ok || !validOwner(owner) || !nameRe.MatchString(name) {
		return Ref{}, ErrInvalid
	}
	return Ref{Owner: owner, Name: name}, nil
}

func validOwner(owner string) bool {
	return len(owner) <= 39 && ownerRe.MatchString(owner)
}

func (r Ref) String() string {
	return r.Owner + "/" + r.Name
}
