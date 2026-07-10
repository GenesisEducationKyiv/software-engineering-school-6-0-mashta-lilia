package subscription

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBodyForLog_RedactsCopy(t *testing.T) {
	body := map[string]any{
		"email": "alice@example.com",
		"repo":  "golang/go",
		"nested": map[string]any{
			"token": "secret-token",
		},
	}

	got := bodyForLog(body)

	assert.Equal(t, "<redacted>", got["email"])
	assert.Equal(t, "golang/go", got["repo"])
	assert.Equal(t, "<redacted>", got["nested"].(map[string]any)["token"])
	assert.Equal(t, "alice@example.com", body["email"])
	assert.Equal(t, "secret-token", body["nested"].(map[string]any)["token"])
}
