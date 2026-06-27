package tracectx_test

import (
	"strings"
	"testing"

	"github-release-notifier/internal/platform/tracectx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewID_Is32HexAndUnique(t *testing.T) {
	t.Parallel()
	id := tracectx.NewID()
	assert.Len(t, id, 32)
	assert.Regexp(t, "^[0-9a-f]{32}$", id)
	assert.True(t, tracectx.IsValidID(id), "a freshly generated id must validate")
	assert.NotEqual(t, id, tracectx.NewID(), "ids must not repeat")
}

func TestIsValidID(t *testing.T) {
	t.Parallel()
	const valid = "0af7651916cd43dd8448eb211c80319c"
	cases := map[string]struct {
		id   string
		want bool
	}{
		"valid":         {valid, true},
		"too short":     {valid[:31], false},
		"too long":      {valid + "0", false},
		"all zero":      {"00000000000000000000000000000000", false},
		"uppercase hex": {"0AF7651916CD43DD8448EB211C80319C", false},
		"non-hex":       {"zzf7651916cd43dd8448eb211c80319c", false},
		"empty":         {"", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tracectx.IsValidID(tc.id))
		})
	}
}

func TestTraceparent_BuildsValidNonZeroParent(t *testing.T) {
	t.Parallel()
	const traceID = "0af7651916cd43dd8448eb211c80319c"
	tp, ok := tracectx.Traceparent(traceID)
	require.True(t, ok)

	parts := strings.Split(tp, "-")
	require.Len(t, parts, 4)
	assert.Equal(t, "00", parts[0])
	assert.Equal(t, traceID, parts[1])
	assert.Regexp(t, "^[0-9a-f]{16}$", parts[2])
	assert.NotEqual(t, "0000000000000000", parts[2], "parent-id must be non-zero per W3C")
	assert.Equal(t, "01", parts[3])
}

func TestTraceparent_RejectsInvalidID(t *testing.T) {
	t.Parallel()
	_, ok := tracectx.Traceparent("not-a-valid-trace-id")
	assert.False(t, ok)
}
