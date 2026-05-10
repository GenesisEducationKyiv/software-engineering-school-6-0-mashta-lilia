package model

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

// ErrInvalidEmail is returned when raw input cannot be parsed as a valid
// RFC 5322 address.
var ErrInvalidEmail = errors.New("invalid email address")

// Email is a normalized, validated email address. Once an Email value
// exists, downstream code can rely on it being lowercase and well-formed,
// so service code does not have to revalidate at every boundary.
type Email struct {
	addr string
}

// NewEmail parses raw input — including RFC 5322 forms like "Alice
// <alice@example.com>" — and returns a normalized lowercase Email.
// The Information Expert for "what makes an email valid" is the email
// type itself, not the service that happens to receive a string.
func NewEmail(raw string) (Email, error) {
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return Email{}, fmt.Errorf("%w: %v", ErrInvalidEmail, err)
	}
	return Email{addr: strings.ToLower(addr.Address)}, nil
}

// String returns the canonical lowercase address — what the service stores
// in the DB and emails to subscribers.
func (e Email) String() string {
	return e.addr
}
