package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedact_SensitiveKeys(t *testing.T) {
	for _, key := range []string{
		"password",
		"token",
		"authorization",
		"secret",
		"api_key",
		"email",
		"jwt",
		"card",
		"cvv",
	} {
		t.Run(key, func(t *testing.T) {
			assert.Equal(t, redactedValue, Redact(key, "secret-value"))
		})
	}
}

func TestRedact_DoesNotMutateInput(t *testing.T) {
	in := map[string]any{
		"email": "alice@example.com",
		"repo":  "golang/go",
		"nested": map[string]any{
			"api_key": "key-123",
		},
	}

	got := Redact("", in).(map[string]any)

	assert.Equal(t, redactedValue, got["email"])
	assert.Equal(t, redactedValue, got["nested"].(map[string]any)["api_key"])
	assert.Equal(t, "alice@example.com", in["email"])
	assert.Equal(t, "key-123", in["nested"].(map[string]any)["api_key"])
}
