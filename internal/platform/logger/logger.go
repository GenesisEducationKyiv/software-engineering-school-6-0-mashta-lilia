package logger

import (
	"context"
	"github-release-notifier/internal/platform/tracectx"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
	"unicode"
)

type Level int

const (
	LevelDebug Level = -4
	LevelInfo  Level = 0
	LevelWarn  Level = 4
	LevelError Level = 8
)

type Config struct {
	Level       string
	ServiceName string
}

type Logger struct {
	logger *slog.Logger
}

func New(cfg Config) *Logger {
	return newWithWriter(cfg, os.Stdout)
}

// NewWithWriter builds a Logger that emits to w. Useful for capturing output in
// tests; production code should use New.
func NewWithWriter(cfg Config, w io.Writer) *Logger {
	return newWithWriter(cfg, w)
}

// Nop returns a Logger that discards everything it is given. Useful as a
// fallback when an optional logger dependency is not supplied.
func Nop() *Logger {
	return newWithWriter(Config{}, io.Discard)
}

func SetDefault(l *Logger) {
	slog.SetDefault(l.logger)
}

func (l *Logger) Debug(ctx context.Context, msg string, kv ...any) {
	l.logger.DebugContext(ctx, msg, kv...)
}

func (l *Logger) Info(ctx context.Context, msg string, kv ...any) {
	l.logger.InfoContext(ctx, msg, kv...)
}

func (l *Logger) Warn(ctx context.Context, msg string, kv ...any) {
	l.logger.WarnContext(ctx, msg, kv...)
}

func (l *Logger) Error(ctx context.Context, msg string, kv ...any) {
	l.logger.ErrorContext(ctx, msg, kv...)
}

func (l *Logger) With(kv ...any) *Logger {
	return &Logger{logger: l.logger.With(kv...)}
}

func (l *Logger) Enabled(ctx context.Context, level Level) bool {
	return l.logger.Enabled(ctx, slog.Level(level))
}

func newWithWriter(cfg Config, w io.Writer) *Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       parseLevel(cfg.Level),
		ReplaceAttr: replaceAttr,
	})
	handlerWithTrace := traceHandler{handler: handler}
	base := slog.New(handlerWithTrace.WithAttrs([]slog.Attr{
		slog.String("service", cfg.ServiceName),
	}))
	return &Logger{logger: base}
}

func parseLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func replaceAttr(_ []string, attr slog.Attr) slog.Attr {
	switch attr.Key {
	case slog.TimeKey:
		attr.Key = "timestamp"
		attr.Value = slog.StringValue(attr.Value.Time().UTC().Format(time.RFC3339Nano))
		return attr
	case slog.LevelKey:
		attr.Value = slog.StringValue(strings.ToLower(attr.Value.String()))
		return attr
	case "err":
		attr.Key = "error"
	default:
		attr.Key = toSnakeCase(attr.Key)
	}
	if isSensitiveKey(attr.Key) {
		attr.Value = slog.StringValue(redactedValue)
		return attr
	}
	if attr.Value.Kind() == slog.KindAny {
		if redacted := redactValue(attr.Value.Any()); redacted != nil {
			attr.Value = slog.AnyValue(redacted)
		}
	}
	return attr
}

func toSnakeCase(key string) string {
	if key == "" {
		return key
	}
	const extraCapacity = 4
	runes := []rune(key)
	var b strings.Builder
	b.Grow(len(key) + extraCapacity)
	var previousUnderscore bool
	for i, r := range runes {
		switch {
		case r == '-' || r == ' ' || r == '.':
			if !previousUnderscore {
				b.WriteByte('_')
				previousUnderscore = true
			}
		case unicode.IsUpper(r):
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			prevLowerOrDigit := i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]))
			if i > 0 && !previousUnderscore && (prevLowerOrDigit || nextLower) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			previousUnderscore = false
		default:
			b.WriteRune(r)
			previousUnderscore = r == '_'
		}
	}
	return b.String()
}

type traceHandler struct {
	handler slog.Handler
}

func (h traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h traceHandler) Handle(ctx context.Context, record slog.Record) error {
	traceID, ok := tracectx.FromContext(ctx)
	if !ok || traceID == "" {
		return h.handler.Handle(ctx, record)
	}
	cloned := record.Clone()
	cloned.AddAttrs(slog.String("trace_id", traceID))
	return h.handler.Handle(ctx, cloned)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{handler: h.handler.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{handler: h.handler.WithGroup(name)}
}
