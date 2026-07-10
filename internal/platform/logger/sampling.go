package logger

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Sampling thins repetitive log lines: keep the first N of each (level, message)
// per window, then every Mth afterward. Tuned so normal low-volume logging is
// untouched and only genuine floods (e.g. a tight retry loop) get reduced.
const (
	defaultSampleFirst      = 20
	defaultSampleThereafter = 50
	defaultSampleInterval   = 10 * time.Second
)

type sampler struct {
	first      int
	thereafter int
	interval   time.Duration
	mu         sync.Mutex
	windows    map[string]*sampleWindow
}

type sampleWindow struct {
	resetAt time.Time
	count   int
}

func newSampler(first, thereafter int, interval time.Duration) *sampler {
	if first <= 0 {
		first = defaultSampleFirst
	}
	if thereafter <= 0 {
		thereafter = defaultSampleThereafter
	}
	if interval <= 0 {
		interval = defaultSampleInterval
	}
	return &sampler{
		first:      first,
		thereafter: thereafter,
		interval:   interval,
		windows:    make(map[string]*sampleWindow),
	}
}

func (s *sampler) allow(level slog.Level, msg string) bool {
	key := level.String() + "\x00" + msg
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	w, ok := s.windows[key]
	if !ok || now.After(w.resetAt) {
		w = &sampleWindow{resetAt: now.Add(s.interval)}
		s.windows[key] = w
	}
	w.count++
	if w.count <= s.first {
		return true
	}
	return (w.count-s.first)%s.thereafter == 0
}

// samplingHandler gates records through the sampler before delegating. Children
// from WithAttrs/WithGroup share one sampler so flood control is global.
type samplingHandler struct {
	inner   slog.Handler
	sampler *sampler
}

func newSamplingHandler(inner slog.Handler, s *sampler) samplingHandler {
	return samplingHandler{inner: inner, sampler: s}
}

func (h samplingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h samplingHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.sampler.allow(r.Level, r.Message) {
		return h.inner.Handle(ctx, r)
	}
	return nil
}

func (h samplingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return samplingHandler{inner: h.inner.WithAttrs(attrs), sampler: h.sampler}
}

func (h samplingHandler) WithGroup(name string) slog.Handler {
	return samplingHandler{inner: h.inner.WithGroup(name), sampler: h.sampler}
}
