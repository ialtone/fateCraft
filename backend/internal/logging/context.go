package logging

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type traceKey struct{}
type fieldsKey struct{}

// NewTrace starts a diagnostic execution, independently of business idempotency.
func NewTrace(ctx context.Context) context.Context {
	return context.WithValue(ctx, traceKey{}, uuid.NewString())
}

func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(traceKey{}).(string)
	return id
}

func WithFields(ctx context.Context, fields ...zap.Field) context.Context {
	previous, _ := ctx.Value(fieldsKey{}).([]zap.Field)
	combined := append(append([]zap.Field(nil), previous...), fields...)
	return context.WithValue(ctx, fieldsKey{}, combined)
}

func FromContext(ctx context.Context, name string) *zap.Logger {
	return withContext(Named(name), ctx)
}

func withContext(log *zap.Logger, ctx context.Context) *zap.Logger {
	if ctx == nil {
		return log
	}
	if id := TraceID(ctx); id != "" {
		log = log.With(zap.String("trace_id", id))
	}
	fields, _ := ctx.Value(fieldsKey{}).([]zap.Field)
	return log.With(fields...)
}
