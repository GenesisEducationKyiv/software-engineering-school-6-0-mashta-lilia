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
		"apiKey",
		"api-key",
		"x-api-key",
		"X-API-Key",
		"authToken",
		"auth_token",
		"userEmail",
		"User-Password",
	} {
		t.Run(key, func(t *testing.T) {
			assert.Equal(t, redactedValue, Redact(key, "secret-value"))
		})
	}
}

func TestRedact_BenignKeys(t *testing.T) {
	for _, key := range []string{
		"repo",
		"owner",
		"tokenizer",
		"tokensize",
		"keyword",
		"emailer",
	} {
		t.Run(key, func(t *testing.T) {
			assert.Equal(t, "not-a-secret", Redact(key, "not-a-secret"),
				"benign key must not be over-redacted")
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

func TestRedact_HTTPHeaderShape(t *testing.T) {
	in := map[string][]string{
		"Authorization": {"Bearer abc123"},
		"User-Agent":    {"curl/8"},
	}

	got := Redact("", in).(map[string][]string)

	assert.Equal(t, []string{redactedValue}, got["Authorization"])
	assert.Equal(t, []string{"curl/8"}, got["User-Agent"])
	assert.Equal(t, []string{"Bearer abc123"}, in["Authorization"])
}
