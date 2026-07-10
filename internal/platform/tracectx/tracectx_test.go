package tracectx_test

import (
	"github-release-notifier/internal/platform/tracectx"
	"strings"
	"testing"

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

func TestParseTraceparent(t *testing.T) {
	t.Parallel()
	const valid = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	cases := map[string]struct {
		header string
		want   string
		ok     bool
	}{
		"valid":              {valid, "0af7651916cd43dd8448eb211c80319c", true},
		"empty":              {"", "", false},
		"too few parts":      {"00-0af7651916cd43dd8448eb211c80319c-01", "", false},
		"all-zero trace id":  {"00-00000000000000000000000000000000-b7ad6b7169203331-01", "", false},
		"non-hex trace id":   {"00-zzf7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01", "", false},
		"short trace id":     {"00-0af7651916cd43dd8448eb211c80319-b7ad6b7169203331-01", "", false},
		"all-zero parent id": {"00-0af7651916cd43dd8448eb211c80319c-0000000000000000-01", "", false},
		"non-hex parent id":  {"00-0af7651916cd43dd8448eb211c80319c-zzzz6b7169203331-01", "", false},
		"non-hex version":    {"zz-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01", "", false},
		"non-hex flags":      {"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-zz", "", false},
		"uppercase is lowered": {
			"00-0AF7651916CD43DD8448EB211C80319C-B7AD6B7169203331-01",
			"0af7651916cd43dd8448eb211c80319c", true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := tracectx.ParseTraceparent(tc.header)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestIsSafeExternalID(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		id   string
		want bool
	}{
		"empty":            {"", false},
		"simple":           {"abc-123_safe.id", true},
		"too long":         {strings.Repeat("a", 65), false},
		"max length":       {strings.Repeat("a", 64), true},
		"contains newline": {"abc\ndef", false},
		"contains space":   {"abc def", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tracectx.IsSafeExternalID(tc.id))
		})
	}
}
