package tracectx

import "context"

type contextKey string

const traceIDKey contextKey = "trace_id"

func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceIDKey, id)
}

func FromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, ok := ctx.Value(traceIDKey).(string)
	return v, ok
}
