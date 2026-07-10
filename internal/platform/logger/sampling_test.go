package logger

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSampler_KeepsFirstThenEveryNth(t *testing.T) {
	t.Parallel()
	s := newSampler(2, 3, time.Hour) // first 2, then every 3rd

	allowed := 0
	for range 10 {
		if s.allow(slog.LevelInfo, "msg") {
			allowed++
		}
	}

	// #1,#2 (first 2) then #5,#8 (every 3rd after) = 4 kept out of 10.
	assert.Equal(t, 4, allowed)
}

func TestSampler_SeparateKeysAreIndependent(t *testing.T) {
	t.Parallel()
	s := newSampler(1, 100, time.Hour)

	assert.True(t, s.allow(slog.LevelInfo, "a"))
	assert.True(t, s.allow(slog.LevelInfo, "b"), "different message is its own bucket")
	assert.True(t, s.allow(slog.LevelWarn, "a"), "different level is its own bucket")
	assert.False(t, s.allow(slog.LevelInfo, "a"), "second of the same key is sampled out")
}

func TestSampler_WindowResets(t *testing.T) {
	t.Parallel()
	s := newSampler(1, 100, time.Millisecond)

	assert.True(t, s.allow(slog.LevelInfo, "x"))
	assert.False(t, s.allow(slog.LevelInfo, "x"))

	time.Sleep(20 * time.Millisecond)
	assert.True(t, s.allow(slog.LevelInfo, "x"), "a new window starts fresh")
}
