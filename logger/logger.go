package logger

import (
	"context"

	"github.com/Maximumsoft-Co-LTD/otelgo/eto"
)

// Info logs an info-level message with optional fields.
// Usage: logger.Info(ctx, "message", "key1", value1, "key2", value2)
func Info(ctx context.Context, msg string, fields ...any) {
	builder := eto.Log().FromContext(ctx).Info().Msg(msg)
	addFields(builder, fields...)
	builder.Send()
}

// Debug logs a debug-level message with optional fields.
// Usage: logger.Debug(ctx, "message", "key1", value1, "key2", value2)
func Debug(ctx context.Context, msg string, fields ...any) {
	builder := eto.Log().FromContext(ctx).Debug().Msg(msg)
	addFields(builder, fields...)
	builder.Send()
}

// Warn logs a warning-level message with optional fields.
// Usage: logger.Warn(ctx, "message", "key1", value1, "key2", value2)
func Warn(ctx context.Context, msg string, fields ...any) {
	builder := eto.Log().FromContext(ctx).Warn().Msg(msg)
	addFields(builder, fields...)
	builder.Send()
}

// Error logs an error-level message with optional fields.
// Usage: logger.Error(ctx, "message", "key1", value1, "key2", value2)
func Error(ctx context.Context, msg string, fields ...any) {
	builder := eto.Log().FromContext(ctx).Error().Msg(msg)
	addFields(builder, fields...)
	builder.Send()
}

// addFields adds key-value pairs to the log builder.
// Fields should be provided as alternating key-value pairs: "key1", value1, "key2", value2, ...
func addFields(builder *eto.LogBuilder, fields ...any) {
	if len(fields)%2 != 0 {
		// If odd number of fields, ignore the last one
		fields = fields[:len(fields)-1]
	}
	for i := 0; i < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok {
			continue
		}
		builder.Field(key, fields[i+1])
	}
}
