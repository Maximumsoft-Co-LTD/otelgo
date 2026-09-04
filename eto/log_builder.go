package eto

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

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

var reservedLogFields = map[string]struct{}{
	"timestamp": {},
	"level":     {},
	"message":   {},
	"trace_id":  {},
	"span_id":   {},
	"payload":   {},
}

var sensitiveLogFieldParts = []string{
	"authorization", "bank", "cookie", "credential", "customer", "password",
	"passwd", "phone", "rawbody", "rawpayload", "requestbody", "responsebody",
	"secret", "tenant", "token", "username",
}

// SafePayload is the only nested object accepted by the log API. Payload
// values must be scalar and keys containing sensitive-data terms are dropped.
// Domain packages remain responsible for constructing this map from a typed
// allowlist rather than forwarding a request or response body.
type SafePayload map[string]any

// Logger is an eto-owned structured logger. NewLogger is intended for an
// isolated sink such as a test buffer; production callers normally use Log so
// the configured OTLP logger and canonical stdout sink are both used.
type Logger struct {
	stdout      *zap.Logger
	otel        otellog.Logger
	stdoutQueue chan logEntry
	otelQueue   chan logEntry
	stdoutDone  chan struct{}
	otelDone    chan struct{}
	mu          sync.RWMutex
	closed      bool
	stats       failureCounters
}

// FailureStats exposes bounded health signals without returning logging
// failures to business callers.
type FailureStats struct {
	Dropped      uint64
	StdoutWrites uint64
	Exports      uint64
	Panics       uint64
}

type failureCounters struct {
	dropped      atomic.Uint64
	stdoutDrops  atomic.Uint64
	otelDrops    atomic.Uint64
	stdoutWrites atomic.Uint64
	exports      atomic.Uint64
	panics       atomic.Uint64
	stdoutPanics atomic.Uint64
	otelPanics   atomic.Uint64
}

type logEntry struct {
	ctx          context.Context
	timestamp    time.Time
	severity     otellog.Severity
	severityText string
	level        LogLevel
	message      string
	fields       []zap.Field
	barrier      chan struct{}
}

type loggerOptions struct{ queueCapacity int }

// LoggerOption configures an isolated eto logger.
type LoggerOption func(*loggerOptions)

// WithQueueCapacity sets the bounded asynchronous queue size.
func WithQueueCapacity(capacity int) LoggerOption {
	return func(options *loggerOptions) {
		if capacity > 0 {
			options.queueCapacity = capacity
		}
	}
}

type logRuntime struct {
	sync.RWMutex
	logger *Logger
}

var (
	defaultLogRuntime logRuntime
	fallbackLogger    = NewLogger(os.Stdout)
)

type LogBuilder struct {
	ctx        context.Context
	level      LogLevel
	msg        string
	fields     []zap.Field
	hasPayload bool
	logger     *Logger
}

// NewLogger creates an isolated canonical JSON logger. It never connects to an
// exporter and is safe to use with an in-memory writer in tests.
func NewLogger(out io.Writer) *Logger {
	return NewLoggerWithOptions(out)
}

// NewLoggerWithOptions creates an isolated logger with a bounded asynchronous
// emission queue. Send never waits for the destination writer or exporter.
func NewLoggerWithOptions(out io.Writer, options ...LoggerOption) *Logger {
	if out == nil {
		out = io.Discard
	}
	settings := loggerOptions{queueCapacity: 256}
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}
	logger := &Logger{
		stdoutQueue: make(chan logEntry, settings.queueCapacity),
		otelQueue:   make(chan logEntry, settings.queueCapacity),
		stdoutDone:  make(chan struct{}),
		otelDone:    make(chan struct{}),
	}
	logger.stdout = newCanonicalJSONLogger(out, &logger.stats)
	go logger.runStdout()
	go logger.runOTLP()
	return logger
}

func newCanonicalJSONLogger(out io.Writer, stats *failureCounters) *zap.Logger {
	encoderConfig := zapcore.EncoderConfig{
		MessageKey:  "message",
		LevelKey:    "level",
		TimeKey:     "timestamp",
		LineEnding:  zapcore.DefaultLineEnding,
		EncodeLevel: zapcore.LowercaseLevelEncoder,
		EncodeTime: func(value time.Time, encoder zapcore.PrimitiveArrayEncoder) {
			encoder.AppendString(value.UTC().Format(time.RFC3339Nano))
		},
	}
	return zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		countingWriteSyncer{writer: zapcore.AddSync(out), stats: stats},
		zapcore.DebugLevel,
	), zap.ErrorOutput(zapcore.AddSync(io.Discard)))
}

type countingWriteSyncer struct {
	writer zapcore.WriteSyncer
	stats  *failureCounters
}

func (writer countingWriteSyncer) Write(data []byte) (int, error) {
	written, err := writer.writer.Write(data)
	if err != nil && writer.stats != nil {
		writer.stats.stdoutWrites.Add(1)
	}
	return written, err
}

func (writer countingWriteSyncer) Sync() error { return writer.writer.Sync() }

func (logger *Logger) runStdout() {
	defer close(logger.stdoutDone)
	for entry := range logger.stdoutQueue {
		if entry.barrier != nil {
			close(entry.barrier)
			continue
		}
		if didPanic(func() {
			safeZapWrite(logger.stdout, entry.timestamp, entry.level, entry.message, entry.fields)
		}) {
			logger.stats.panics.Add(1)
			logger.stats.stdoutPanics.Add(1)
		}
	}
}

func (logger *Logger) runOTLP() {
	defer close(logger.otelDone)
	for entry := range logger.otelQueue {
		if entry.barrier != nil {
			close(entry.barrier)
			continue
		}
		if didPanic(func() {
			safeOtelEmit(logger.otel, entry.ctx, entry.timestamp, entry.severity, entry.severityText, entry.message, entry.fields)
		}) {
			logger.stats.panics.Add(1)
			logger.stats.otelPanics.Add(1)
		}
	}
}

func didPanic(operation func()) (panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	operation()
	return false
}

func (logger *Logger) enqueue(entry logEntry) {
	if logger == nil {
		return
	}
	logger.mu.RLock()
	defer logger.mu.RUnlock()
	if logger.closed {
		logger.stats.dropped.Add(1)
		logger.stats.stdoutDrops.Add(1)
		logger.stats.otelDrops.Add(1)
		return
	}
	dropped := false
	select {
	case logger.otelQueue <- entry:
	default:
		logger.stats.otelDrops.Add(1)
		dropped = true
	}
	select {
	case logger.stdoutQueue <- entry:
	default:
		logger.stats.stdoutDrops.Add(1)
		dropped = true
	}
	if dropped {
		logger.stats.dropped.Add(1)
	}
}

// Flush waits until records already accepted by both delivery queues have
// reached their sink. A failure or blockage in one sink never prevents the
// other worker from making progress, though Flush still reports its deadline
// if either sink has not reached the barrier.
func (logger *Logger) Flush(ctx context.Context) error {
	if logger == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	logger.mu.RLock()
	if logger.closed {
		logger.mu.RUnlock()
		return waitForWorkers(ctx, logger.otelDone, logger.stdoutDone)
	}
	otelBarrier := make(chan struct{})
	stdoutBarrier := make(chan struct{})
	select {
	case logger.otelQueue <- logEntry{barrier: otelBarrier}:
	case <-ctx.Done():
		logger.mu.RUnlock()
		return ctx.Err()
	}
	select {
	case logger.stdoutQueue <- logEntry{barrier: stdoutBarrier}:
	case <-ctx.Done():
		logger.mu.RUnlock()
		return ctx.Err()
	}
	logger.mu.RUnlock()
	return waitForWorkers(ctx, otelBarrier, stdoutBarrier)
}

// Shutdown stops accepting records and waits, subject to ctx, for accepted
// records to drain. A blocked sink cannot hold the caller past its deadline.
func (logger *Logger) Shutdown(ctx context.Context) error {
	if logger == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !logger.lockUntil(ctx) {
		return ctx.Err()
	}
	if !logger.closed {
		logger.closed = true
		close(logger.otelQueue)
		close(logger.stdoutQueue)
	}
	logger.mu.Unlock()
	return waitForWorkers(ctx, logger.otelDone, logger.stdoutDone)
}

func waitForWorkers(ctx context.Context, workers ...<-chan struct{}) error {
	for _, worker := range workers {
		select {
		case <-worker:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (logger *Logger) lockUntil(ctx context.Context) bool {
	if logger.mu.TryLock() {
		return true
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if logger.mu.TryLock() {
				return true
			}
		}
	}
}

// Failures returns a race-free snapshot of logger health counters.
func (logger *Logger) Failures() FailureStats {
	if logger == nil {
		return FailureStats{}
	}
	return FailureStats{
		Dropped:      logger.stats.dropped.Load(),
		StdoutWrites: logger.stats.stdoutWrites.Load(),
		Exports:      logger.stats.exports.Load(),
		Panics:       logger.stats.panics.Load(),
	}
}

func setDefaultLogger(logger *Logger) {
	defaultLogRuntime.Lock()
	defaultLogRuntime.logger = logger
	defaultLogRuntime.Unlock()
}

func currentLogger() *Logger {
	defaultLogRuntime.RLock()
	logger := defaultLogRuntime.logger
	defaultLogRuntime.RUnlock()
	if logger == nil {
		return fallbackLogger
	}
	return logger
}

// SetLoggerForTesting swaps the process-wide logger and returns a restore
// function. Tests using it must not run in parallel with other global-logger
// tests. Runtime code should use Init instead.
func SetLoggerForTesting(logger *Logger) (restore func()) {
	defaultLogRuntime.Lock()
	previous := defaultLogRuntime.logger
	defaultLogRuntime.logger = logger
	defaultLogRuntime.Unlock()
	return func() {
		defaultLogRuntime.Lock()
		defaultLogRuntime.logger = previous
		defaultLogRuntime.Unlock()
	}
}

// DefaultLoggerFailures returns process-wide queue, sink, exporter, and panic
// counters for health reporting without coupling business paths to log errors.
func DefaultLoggerFailures() FailureStats { return currentLogger().Failures() }

// DefaultLoggerFailureTotal returns one monotonic, scrape-safe health value
// for Prometheus collectors in services that do not enable application OTel
// metrics. Reading it performs only atomic loads.
func DefaultLoggerFailureTotal() uint64 {
	stats := DefaultLoggerFailures()
	total := stats.Dropped
	for _, value := range []uint64{stats.StdoutWrites, stats.Exports, stats.Panics} {
		if ^uint64(0)-total < value {
			return ^uint64(0)
		}
		total += value
	}
	return total
}

// Log starts a record on the process-wide eto runtime.
func Log() *LogBuilder { return currentLogger().Log() }

// Flush waits for records already accepted by the process-wide logger. It is
// intended for bounded process shutdown; business paths must not call it.
func Flush(ctx context.Context) error { return currentLogger().Flush(ctx) }

// Log starts a record on this isolated eto logger.
func (logger *Logger) Log() *LogBuilder {
	if logger == nil {
		logger = fallbackLogger
	}
	return &LogBuilder{
		ctx:    context.Background(),
		level:  levelInfo,
		logger: logger,
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

// Level maps a dynamic legacy level onto the four canonical eto levels.
// Unknown values are informational so malformed metadata cannot manufacture
// an operator-actionable error.
func (b *LogBuilder) Level(level string) *LogBuilder {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return b.Debug()
	case "warn", "warning":
		return b.Warn()
	case "error", "invalid":
		return b.Error()
	default:
		return b.Info()
	}
}

func (b *LogBuilder) Msg(msg string) *LogBuilder {
	b.msg = msg
	return b
}

// Field adds one top-level scalar extension. Reserved envelope fields,
// sensitive keys, nested values, raw errors, and arbitrary objects are
// rejected instead of being serialized accidentally.
func (b *LogBuilder) Field(key string, value any) *LogBuilder {
	field, ok := safeZapField(key, value)
	if ok {
		b.fields = append(b.fields, field)
	}
	return b
}

// Payload adds a sanitized JSON object. Only safe scalar values survive.
func (b *LogBuilder) Payload(payload SafePayload) *LogBuilder {
	safe := make(map[string]any, len(payload))
	for key, value := range payload {
		if normalized, ok := safeScalarValue(key, value); ok {
			safe[key] = normalized
		}
	}
	b.fields = append(b.fields, zap.Reflect("payload", safe))
	b.hasPayload = true
	return b
}

// Fields preserves the original zap-based API for scalar fields while
// applying the same key and value restrictions as Field.
func (b *LogBuilder) Fields(fields ...zap.Field) *LogBuilder {
	for _, field := range fields {
		if safe, ok := sanitizeZapField(field); ok {
			b.fields = append(b.fields, safe)
		}
	}
	return b
}

func safeZapField(key string, value any) (zap.Field, bool) {
	normalized, ok := safeScalarValue(key, value)
	if !ok {
		return zap.Field{}, false
	}
	switch value := normalized.(type) {
	case string:
		return zap.String(key, value), true
	case bool:
		return zap.Bool(key, value), true
	case int64:
		return zap.Int64(key, value), true
	case uint64:
		return zap.Uint64(key, value), true
	case float64:
		return zap.Float64(key, value), true
	default:
		return zap.Field{}, false
	}
}

func safeScalarValue(key string, value any) (any, bool) {
	if !safeLogKey(key) {
		return nil, false
	}
	switch value := value.(type) {
	case string:
		return value, true
	case bool:
		return value, true
	case int:
		return int64(value), true
	case int8:
		return int64(value), true
	case int16:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case uint:
		return uint64(value), true
	case uint8:
		return uint64(value), true
	case uint16:
		return uint64(value), true
	case uint32:
		return uint64(value), true
	case uint64:
		return value, true
	case float32:
		return float64(value), true
	case float64:
		return value, true
	case time.Duration:
		return value.Milliseconds(), true
	case time.Time:
		return value.UTC().Format(time.RFC3339Nano), true
	default:
		return nil, false
	}
}

func safeLogKey(key string) bool {
	if key == "" || len(key) > 64 {
		return false
	}
	if _, reserved := reservedLogFields[key]; reserved {
		return false
	}
	for index, char := range key {
		if (index == 0 && !unicode.IsLower(char)) ||
			(index > 0 && char != '_' && !unicode.IsLower(char) && !unicode.IsDigit(char)) {
			return false
		}
	}
	normalized := strings.ReplaceAll(strings.ToLower(key), "_", "")
	for _, part := range sensitiveLogFieldParts {
		if strings.Contains(normalized, part) {
			return false
		}
	}
	return true
}

func sanitizeZapField(field zap.Field) (zap.Field, bool) {
	if !safeLogKey(field.Key) {
		return zap.Field{}, false
	}
	switch field.Type {
	case zapcore.StringType:
		return zap.String(field.Key, field.String), true
	case zapcore.BoolType:
		return zap.Bool(field.Key, field.Integer == 1), true
	case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type:
		return zap.Int64(field.Key, field.Integer), true
	case zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
		return zap.Uint64(field.Key, uint64(field.Integer)), true
	case zapcore.Float64Type:
		return zap.Float64(field.Key, math.Float64frombits(uint64(field.Integer))), true
	case zapcore.Float32Type:
		return zap.Float64(field.Key, float64(math.Float32frombits(uint32(field.Integer)))), true
	default:
		return zap.Field{}, false
	}
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

func logCaller() string {
	const (
		maxDepth   = 32
		skipFrames = 3
	)

	pcs := make([]uintptr, maxDepth)
	n := runtime.Callers(skipFrames, pcs)
	if n == 0 {
		return ""
	}

	frames := runtime.CallersFrames(pcs[:n])

	for {
		frame, more := frames.Next()

		if useFrame(frame) {
			file := filepath.Base(frame.File)
			funcName := shortFuncName(frame.Function)
			return fmt.Sprintf("%s:%d %s", file, frame.Line, funcName)
		}

		if !more {
			break
		}
	}

	return ""
}

func useFrame(frame runtime.Frame) bool {
	if frame.File == "" && frame.Function == "" {
		return false
	}

	runtimeMu.RLock()
	cfg := globalCfg
	runtimeMu.RUnlock()

	for _, p := range cfg.SkipCallerPkgs {
		if p != "" && strings.HasPrefix(frame.Function, p) {
			return false
		}
	}

	for _, f := range cfg.SkipCallerFiles {
		if f != "" && strings.Contains(frame.File, f) {
			return false
		}
	}

	return true
}

func shortFuncName(fn string) string {
	if fn == "" {
		return ""
	}

	if idx := strings.LastIndex(fn, "/"); idx >= 0 && idx < len(fn)-1 {
		fn = fn[idx+1:]
	}
	return fn
}
func (b *LogBuilder) Send() {
	if b == nil {
		return
	}
	ctx := b.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	message := b.msg
	if message == "" {
		message = "no-message"
	}
	timestamp := time.Now().UTC()
	spanContext := trace.SpanContextFromContext(ctx)
	fields := append([]zap.Field(nil), b.fields...)
	if !b.hasPayload {
		fields = append(fields, zap.Reflect("payload", map[string]any{}))
	}
	if spanContext.IsValid() {
		fields = append(fields,
			zap.String("trace_id", spanContext.TraceID().String()),
			zap.String("span_id", spanContext.SpanID().String()),
		)
	}
	if caller := logCaller(); caller != "" {
		fields = append(fields, zap.String("caller", caller))
	}
	logger := b.logger
	if logger == nil {
		logger = currentLogger()
	}
	logger.enqueue(logEntry{
		ctx:          ctx,
		timestamp:    timestamp,
		severity:     b.otelSeverity(),
		severityText: b.severityText(),
		level:        b.level,
		message:      message,
		fields:       fields,
	})
}

func safeOtelEmit(logger otellog.Logger, ctx context.Context, timestamp time.Time, severity otellog.Severity, severityText, message string, fields []zap.Field) {
	if logger == nil {
		return
	}
	var record otellog.Record
	record.SetTimestamp(timestamp)
	record.SetObservedTimestamp(timestamp)
	record.SetSeverity(severity)
	record.SetSeverityText(severityText)
	record.SetBody(otellog.StringValue(message))
	record.AddAttributes(zapFieldsToOtelAttrs(fields)...)
	logger.Emit(ctx, record)
}

func safeZapWrite(logger *zap.Logger, timestamp time.Time, level LogLevel, message string, fields []zap.Field) {
	if logger == nil {
		return
	}
	zapLevel := zapcore.InfoLevel
	switch level {
	case levelDebug:
		zapLevel = zapcore.DebugLevel
	case levelWarn:
		zapLevel = zapcore.WarnLevel
	case levelError:
		zapLevel = zapcore.ErrorLevel
	}
	if checked := logger.Check(zapLevel, message); checked != nil {
		checked.Time = timestamp
		checked.Write(fields...)
	}
}

func zapFieldsToOtelAttrs(fields []zap.Field) []otellog.KeyValue {
	attrs := make([]otellog.KeyValue, 0, len(fields))
	for _, field := range fields {
		switch field.Type {
		case zapcore.StringType:
			attrs = append(attrs, otellog.String(field.Key, field.String))
		case zapcore.BoolType:
			attrs = append(attrs, otellog.Bool(field.Key, field.Integer == 1))
		case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type:
			attrs = append(attrs, otellog.Int64(field.Key, field.Integer))
		case zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
			if field.Integer >= 0 {
				attrs = append(attrs, otellog.Int64(field.Key, field.Integer))
			}
		case zapcore.Float64Type:
			attrs = append(attrs, otellog.Float64(field.Key, math.Float64frombits(uint64(field.Integer))))
		case zapcore.Float32Type:
			attrs = append(attrs, otellog.Float64(field.Key, float64(math.Float32frombits(uint32(field.Integer)))))
		case zapcore.ReflectType:
			if field.Key == "payload" {
				if payload, ok := field.Interface.(map[string]any); ok {
					attrs = append(attrs, safePayloadOtelAttribute(payload))
				}
			}
		}
	}
	return attrs
}

func safePayloadOtelAttribute(payload map[string]any) otellog.KeyValue {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	attrs := make([]otellog.KeyValue, 0, len(keys))
	for _, key := range keys {
		switch value := payload[key].(type) {
		case string:
			attrs = append(attrs, otellog.String(key, value))
		case bool:
			attrs = append(attrs, otellog.Bool(key, value))
		case int64:
			attrs = append(attrs, otellog.Int64(key, value))
		case uint64:
			if value <= math.MaxInt64 {
				attrs = append(attrs, otellog.Int64(key, int64(value)))
			}
		case float64:
			attrs = append(attrs, otellog.Float64(key, value))
		}
	}
	return otellog.Map("payload", attrs...)
}
