package email

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

var ErrInvalid = errors.New("invalid email address")

type Address struct {
	addr string
}

// NewAddress accepts RFC 5322 forms such as "Alice <alice@example.com>"
// and returns the lowercased address.
func NewAddress(raw string) (Address, error) {
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return Address{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return Address{addr: strings.ToLower(addr.Address)}, nil
}

func (a Address) String() string {
	return a.addr
}
