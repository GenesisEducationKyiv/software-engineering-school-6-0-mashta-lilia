package token_test

import (
	"encoding/hex"
	"testing"

	"github-release-notifier/internal/platform/token"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerator_Generate_ProducesHexOfExpectedLength(t *testing.T) {
	t.Parallel()
	got, err := token.New().Generate()
	require.NoError(t, err)

	assert.Len(t, got, 64)

	decoded, err := hex.DecodeString(got)
	require.NoError(t, err, "token must be valid hex")
	assert.Len(t, decoded, 32)
}

func TestGenerator_Generate_URLSafeCharset(t *testing.T) {
	t.Parallel()
	tok, err := token.New().Generate()
	require.NoError(t, err)
	for _, c := range tok {
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		assert.True(t, isHex, "non-hex char %q", c)
	}
}
