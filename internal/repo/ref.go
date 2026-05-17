package repo

import (
	"errors"
	"regexp"
	"strings"
)

var ErrInvalid = errors.New("invalid repository format, expected owner/repo")

// GitHub owner: letters/digits/hyphens, 1-39 chars (GitHub's documented limit).
var ownerRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)

// GitHub repo name: letters/digits and ._- , 1-100 chars (GitHub allows this set).
var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

type Ref struct {
	Owner string
	Name  string
}

// ParseRef enforces GitHub's actual owner/name charset. The strict regex
// blocks control characters (CR/LF) so the value is safe to interpolate
// into log lines, URLs, and email headers without further escaping.
func ParseRef(raw string) (Ref, error) {
	owner, name, ok := strings.Cut(raw, "/")
	if !ok || !ownerRe.MatchString(owner) || !nameRe.MatchString(name) {
		return Ref{}, ErrInvalid
	}
	return Ref{Owner: owner, Name: name}, nil
}

func (r Ref) String() string {
	return r.Owner + "/" + r.Name
}
