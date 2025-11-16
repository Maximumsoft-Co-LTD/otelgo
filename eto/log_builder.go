package eto

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type LogLevel int

const (
	levelDebug LogLevel = iota
	levelInfo
	levelWarn
	levelError
)

type LogBuilder struct {
	ctx    context.Context
	level  LogLevel
	msg    string
	fields []zap.Field
}

// Log เริ่ม Fluent logger
func Log() *LogBuilder {
	return &LogBuilder{
		ctx:   context.Background(),
		level: levelInfo,
	}
}

func (b *LogBuilder) FromContext(ctx context.Context) *LogBuilder {
	if ctx != nil {
		b.ctx = ctx
	}
	return b
}

func (b *LogBuilder) Debug() *LogBuilder {
	b.level = levelDebug
	return b
}

func (b *LogBuilder) Info() *LogBuilder {
	b.level = levelInfo
	return b
}

func (b *LogBuilder) Warn() *LogBuilder {
	b.level = levelWarn
	return b
}

func (b *LogBuilder) Error() *LogBuilder {
	b.level = levelError
	return b
}

func (b *LogBuilder) Msg(msg string) *LogBuilder {
	b.msg = msg
	return b
}

func (b *LogBuilder) Field(key string, val any) *LogBuilder {
	switch v := val.(type) {
	case string:
		b.fields = append(b.fields, zap.String(key, v))
	case int:
		b.fields = append(b.fields, zap.Int(key, v))
	case int64:
		b.fields = append(b.fields, zap.Int64(key, v))
	case float64:
		b.fields = append(b.fields, zap.Float64(key, v))
	case bool:
		b.fields = append(b.fields, zap.Bool(key, v))
	default:
		b.fields = append(b.fields, zap.Any(key, v))
	}
	return b
}

func (b *LogBuilder) Fields(fields ...zap.Field) *LogBuilder {
	b.fields = append(b.fields, fields...)
	return b
}

func (b *LogBuilder) Send() {
	if globalLogger == nil {
		return
	}

	// ผูก trace_id/span_id จาก ctx
	span := trace.SpanFromContext(b.ctx)
	if span != nil {
		sc := span.SpanContext()
		if sc.IsValid() {
			b.fields = append(b.fields,
				zap.String("trace_id", sc.TraceID().String()),
				zap.String("span_id", sc.SpanID().String()),
			)
		}
	}

	if b.msg == "" {
		b.msg = "no-message"
	}

	switch b.level {
	case levelDebug:
		globalLogger.Debug(b.msg, b.fields...)
	case levelInfo:
		globalLogger.Info(b.msg, b.fields...)
	case levelWarn:
		globalLogger.Warn(b.msg, b.fields...)
	case levelError:
		globalLogger.Error(b.msg, b.fields...)
	}
}
