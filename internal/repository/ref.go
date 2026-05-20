package repository

import (
	"errors"
	"regexp"
	"strings"
)

var ErrInvalid = errors.New("invalid repository format, expected owner/repo")

// GitHub repo name: letters/digits and ._- , 1-100 chars (GitHub allows this set).
var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

// GitHub owner: alnum start, then any number of "(optional -) + alnum".
// Rejects empty, leading -, trailing -, and consecutive -- by construction.
// Length cap (39) is checked separately in validOwner.
var ownerRe = regexp.MustCompile(`^[A-Za-z0-9](?:-?[A-Za-z0-9])*$`)

type Ref struct {
	Owner string
	Name  string
}

// ParseRef enforces GitHub's actual owner/name charset. The strict regex
// blocks control characters (CR/LF) so the value is safe to interpolate
// into log lines, URLs, and email headers without further escaping.
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
