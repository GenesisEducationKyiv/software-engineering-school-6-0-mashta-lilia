package token

import (
	"crypto/rand"
	"encoding/hex"
)

// 256 bits of entropy.
const tokenBytes = 32

type Generator struct{}

func New() Generator {
	return Generator{}
}

func (Generator) Generate() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
