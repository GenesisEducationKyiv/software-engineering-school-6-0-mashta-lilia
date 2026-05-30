package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
	"unicode"

	"github-release-notifier/internal/platform/tracectx"
)

type Logger interface {
	Debug(ctx context.Context, msg string, kv ...any)
	Info(ctx context.Context, msg string, kv ...any)
	Warn(ctx context.Context, msg string, kv ...any)
	Error(ctx context.Context, msg string, kv ...any)
	With(kv ...any) Logger
}

type Config struct {
	Level       string
	ServiceName string
}

type slogLogger struct {
	logger *slog.Logger
}

func New(cfg Config) Logger {
	return newWithWriter(cfg, os.Stdout)
}

func SetDefault(l Logger) {
	if log, ok := l.(*slogLogger); ok {
		slog.SetDefault(log.logger)
	}
}

func (l *slogLogger) Debug(ctx context.Context, msg string, kv ...any) {
	l.logger.DebugContext(ctx, msg, kv...)
}

func (l *slogLogger) Info(ctx context.Context, msg string, kv ...any) {
	l.logger.InfoContext(ctx, msg, kv...)
}

func (l *slogLogger) Warn(ctx context.Context, msg string, kv ...any) {
	l.logger.WarnContext(ctx, msg, kv...)
}

func (l *slogLogger) Error(ctx context.Context, msg string, kv ...any) {
	l.logger.ErrorContext(ctx, msg, kv...)
}

func (l *slogLogger) With(kv ...any) Logger {
	return &slogLogger{logger: l.logger.With(kv...)}
}

func newWithWriter(cfg Config, w io.Writer) Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       parseLevel(cfg.Level),
		ReplaceAttr: replaceAttr,
	})
	handlerWithTrace := traceHandler{handler: handler}
	base := slog.New(handlerWithTrace.WithAttrs([]slog.Attr{
		slog.String("service", cfg.ServiceName),
	}))
	return &slogLogger{logger: base}
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
	case slog.LevelKey:
		attr.Value = slog.StringValue(strings.ToLower(attr.Value.String()))
	case "err":
		attr.Key = "error"
	default:
		attr.Key = toSnakeCase(attr.Key)
	}
	return attr
}

func toSnakeCase(key string) string {
	if key == "" {
		return key
	}
	runes := []rune(key)
	var b strings.Builder
	b.Grow(len(key) + 4)
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
	if traceID, ok := tracectx.FromContext(ctx); ok && traceID != "" {
		record.AddAttrs(slog.String("trace_id", traceID))
	}
	return h.handler.Handle(ctx, record)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{handler: h.handler.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{handler: h.handler.WithGroup(name)}
}
