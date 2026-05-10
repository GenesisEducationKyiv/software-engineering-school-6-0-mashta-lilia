package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// TokenGenerator produces opaque tokens used for confirmation/unsubscribe
// links. Defining this as an interface (rather than calling crypto/rand
// directly inside SubscriptionService) lets tests substitute deterministic
// generators and lets us swap the entropy source without touching business
// logic.
type TokenGenerator interface {
	Generate() (string, error)
}

// CryptoTokenGenerator returns 32 bytes from crypto/rand encoded as a 64-char
// hex string (256 bits of entropy). This is the production implementation.
type CryptoTokenGenerator struct{}

func (CryptoTokenGenerator) Generate() (string, error) {
	const tokenBytes = 32
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
