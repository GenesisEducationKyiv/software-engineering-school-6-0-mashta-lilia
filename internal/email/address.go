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

func NewAddress(raw string) (Address, error) {
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return Address{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return Address{addr: strings.ToLower(addr.Address)}, nil
}

func (a Address) String() string {
	return a.addr
}
