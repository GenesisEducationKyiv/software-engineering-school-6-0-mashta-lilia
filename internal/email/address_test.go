package email_test

import (
	"errors"
	"testing"

	"github-release-notifier/internal/email"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAddress_ValidEmails(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want string
	}{
		{"alice@example.com", "alice@example.com"},
		{"Alice@Example.COM", "alice@example.com"},
		{"Alice <alice@example.com>", "alice@example.com"},
		{`"Display Name" <alice@example.com>`, "alice@example.com"},
		{"a.b+tag@example.co.uk", "a.b+tag@example.co.uk"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			a, err := email.NewAddress(tc.raw)
			require.NoError(t, err)
			assert.Equal(t, tc.want, a.String())
		})
	}
}

func TestNewAddress_InvalidEmails(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"not-an-email",
		"@example.com",
		"alice@",
		"alice example.com",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			_, err := email.NewAddress(raw)
			require.Error(t, err)
			assert.True(t, errors.Is(err, email.ErrInvalid),
				"expected ErrInvalid for %q, got %v", raw, err)
		})
	}
}
