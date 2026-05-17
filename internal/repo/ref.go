package repo

import (
	"errors"
	"strings"
)

var ErrInvalid = errors.New("invalid repository format, expected owner/repo")

type Ref struct {
	Owner string
	Name  string
}

func ParseRef(raw string) (Ref, error) {
	owner, name, ok := strings.Cut(raw, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return Ref{}, ErrInvalid
	}
	return Ref{Owner: owner, Name: name}, nil
}

func (r Ref) String() string {
	return r.Owner + "/" + r.Name
}
