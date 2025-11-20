package eto

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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

func (b *LogBuilder) Debug() *LogBuilder { b.level = levelDebug; return b }
func (b *LogBuilder) Info() *LogBuilder  { b.level = levelInfo; return b }
func (b *LogBuilder) Warn() *LogBuilder  { b.level = levelWarn; return b }
func (b *LogBuilder) Error() *LogBuilder { b.level = levelError; return b }

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

func (b *LogBuilder) otelSeverity() otellog.Severity {
	switch b.level {
	case levelDebug:
		return otellog.SeverityDebug
	case levelInfo:
		return otellog.SeverityInfo
	case levelWarn:
		return otellog.SeverityWarn
	case levelError:
		return otellog.SeverityError
	default:
		return otellog.SeverityInfo
	}
}

func (b *LogBuilder) severityText() string {
	switch b.level {
	case levelDebug:
		return "DEBUG"
	case levelInfo:
		return "INFO"
	case levelWarn:
		return "WARN"
	case levelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

func logCaller(skip int) string {
	pc, file, line, ok := runtime.Caller(skip)
	if !ok {
		return ""
	}
	fn := runtime.FuncForPC(pc)
	if fn != nil {
		return fmt.Sprintf("%s:%d %s", filepath.Base(file), line, fn.Name())
	}
	return fmt.Sprintf("%s:%d", filepath.Base(file), line)
}

func (b *LogBuilder) Send() {
	ctx := b.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	msg := b.msg
	if msg == "" {
		msg = "no-message"
	}

	now := time.Now().UTC()
	levelText := b.severityText()
	caller := logCaller(2)

	span := trace.SpanFromContext(ctx)
	sc := span.SpanContext()

	b.fields = append(b.fields,
		zap.String("level", levelText),
		zap.Time("ts", now),
	)
	if caller != "" {
		b.fields = append(b.fields, zap.String("caller", caller))
	}
	if sc.IsValid() {
		b.fields = append(b.fields,
			zap.String("trace_id", sc.TraceID().String()),
			zap.String("span_id", sc.SpanID().String()),
		)
	}

	if globalOtelLogger != nil {
		var rec otellog.Record

		rec.SetSeverity(b.otelSeverity())
		rec.SetSeverityText(levelText)

		rec.SetBody(otellog.StringValue(msg))

		for _, a := range zapFieldsToOtelAttrs(b.fields) {
			rec.AddAttributes(a)
		}

		rec.AddAttributes(
			otellog.String("level", levelText),
		)
		if caller != "" {
			rec.AddAttributes(otellog.String("caller", caller))
		}
		if sc.IsValid() {
			rec.AddAttributes(
				otellog.String("trace_id", sc.TraceID().String()),
				otellog.String("span_id", sc.SpanID().String()),
			)
		}

		rec.SetTimestamp(now)
		rec.SetObservedTimestamp(now)

		rec.AddAttributes(
			otellog.Int64("ts_unix_nano", now.UnixNano()),
		)

		globalOtelLogger.Emit(ctx, rec)
	}

	if globalLogger == nil {
		return
	}

	switch b.level {
	case levelDebug:
		globalLogger.Debug(msg, b.fields...)
	case levelInfo:
		globalLogger.Info(msg, b.fields...)
	case levelWarn:
		globalLogger.Warn(msg, b.fields...)
	case levelError:
		globalLogger.Error(msg, b.fields...)
	}
}

func zapFieldsToOtelAttrs(fields []zap.Field) []otellog.KeyValue {
	attrs := make([]otellog.KeyValue, 0, len(fields))

	for _, f := range fields {
		switch f.Type {
		case zapcore.StringType:
			attrs = append(attrs, otellog.String(f.Key, f.String))
		case zapcore.BoolType:
			attrs = append(attrs, otellog.Bool(f.Key, f.Integer == 1))
		case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type,
			zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
			attrs = append(attrs, otellog.Int64(f.Key, f.Integer))
		case zapcore.TimeType:
			attrs = append(attrs, otellog.Int64(f.Key, f.Integer))
		default:
			if f.String != "" {
				attrs = append(attrs, otellog.String(f.Key, f.String))
			}
		}
	}

	return attrs
}
