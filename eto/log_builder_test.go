package eto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"
)

func TestLoggerEmitsCanonicalEnvelopeFromActiveSpan(t *testing.T) {
	// DIRECT-ETO-CANONICAL-LOGS binds this executable case to the evidence contract.
	var output bytes.Buffer
	logger := NewLogger(&output)
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36},
		SpanID:     trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

	logger.Log().FromContext(ctx).Error().Msg("payment authorization failed").
		Field("http_method", "POST").
		Field("http_route", "/v1/orders/{id}/pay").
		Field("status_code", 502).
		Field("duration_ms", int64(1840)).
		Field("order_id", "ord_8812").
		Field("error_type", "UpstreamTimeout").
		Payload(SafePayload{}).
		Send()
	flushLogger(t, logger)

	record := decodeLogRecord(t, output.Bytes())
	if _, err := time.Parse(time.RFC3339Nano, record["timestamp"].(string)); err != nil {
		t.Fatalf("timestamp is not RFC3339Nano: %v", err)
	}
	assertLogValue(t, record, "level", "error")
	assertLogValue(t, record, "message", "payment authorization failed")
	assertLogValue(t, record, "trace_id", spanContext.TraceID().String())
	assertLogValue(t, record, "span_id", spanContext.SpanID().String())
	assertLogValue(t, record, "status_code", float64(502))
	assertLogValue(t, record, "duration_ms", float64(1840))
	caller, ok := record["caller"].(string)
	if !ok || caller == "" {
		t.Fatalf("caller = %#v, want a non-empty string", record["caller"])
	}
	if _, ok := record["payload"].(map[string]any); !ok {
		t.Fatalf("payload type = %T, want JSON object", record["payload"])
	}
	for _, legacyKey := range []string{"ts", "msg"} {
		if _, exists := record[legacyKey]; exists {
			t.Fatalf("legacy key %q must not be emitted", legacyKey)
		}
	}
}

func TestLoggerProtectsReservedAndSensitiveFields(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output)

	logger.Log().Info().Msg("safe message").
		Field("timestamp", "attacker-time").
		Field("level", "error").
		Field("message", "attacker-message").
		Field("trace_id", "attacker-trace").
		Field("span_id", "attacker-span").
		Field("payload", "attacker-payload").
		Field("authorization", "Bearer secret").
		Field("raw_payload", map[string]any{"token": "secret"}).
		Payload(SafePayload{"operation": "cashback_claim", "token": "secret", "nested": map[string]any{"password": "secret"}}).
		Send()
	flushLogger(t, logger)

	record := decodeLogRecord(t, output.Bytes())
	assertLogValue(t, record, "level", "info")
	assertLogValue(t, record, "message", "safe message")
	for _, forbidden := range []string{"trace_id", "span_id", "authorization", "raw_payload"} {
		if _, exists := record[forbidden]; exists {
			t.Fatalf("forbidden field %q was emitted", forbidden)
		}
	}
	payload, ok := record["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want JSON object", record["payload"])
	}
	if len(payload) != 1 || payload["operation"] != "cashback_claim" {
		t.Fatalf("payload = %#v, want only the safe scalar", payload)
	}
}

func TestLoggerWriteFailureDoesNotPanic(t *testing.T) {
	logger := NewLogger(errorWriter{})
	logger.Log().Error().Msg("telemetry failure").Field("error_type", "SinkUnavailable").Send()
	flushLogger(t, logger)
	if got := logger.Failures().StdoutWrites; got != 1 {
		t.Fatalf("stdout write failures = %d, want 1", got)
	}
}

func TestLoggerAddsEmptyPayloadToDefaultEnvelope(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output)
	logger.Log().Info().Msg("default envelope").Send()
	flushLogger(t, logger)

	record := decodeLogRecord(t, output.Bytes())
	payload, ok := record["payload"].(map[string]any)
	if !ok || len(payload) != 0 {
		t.Fatalf("payload = %#v, want an empty JSON object", record["payload"])
	}
}

func TestFailureCountingLogExporterReportsExportFailure(t *testing.T) {
	var counters failureCounters
	exporter := &failureCountingLogExporter{Exporter: failingLogExporter{}, stats: &counters}
	if err := exporter.Export(context.Background(), []sdklog.Record{{}}); err == nil {
		t.Fatal("Export error = nil, want failure")
	}
	if got := counters.exports.Load(); got != 1 {
		t.Fatalf("export failures = %d, want 1", got)
	}
}

func TestSetLoggerForTestingCapturesDirectGlobalLogCall(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output)
	restore := SetLoggerForTesting(logger)
	defer restore()

	Log().Info().Msg("direct eto call").Field("operation", "test").Send()
	flushLogger(t, logger)
	record := decodeLogRecord(t, output.Bytes())
	assertLogValue(t, record, "message", "direct eto call")
}

func TestLogBuilderLevelMapsDynamicLevels(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{"DEBUG", "debug"},
		{"info", "info"},
		{" warning ", "warn"},
		{"INVALID", "error"},
		{"ERROR", "error"},
		{"unknown", "info"},
	} {
		t.Run(test.input, func(t *testing.T) {
			var output bytes.Buffer
			logger := NewLogger(&output)
			logger.Log().Level(test.input).Msg("dynamic level").Send()
			flushLogger(t, logger)
			assertLogValue(t, decodeLogRecord(t, output.Bytes()), "level", test.want)
		})
	}
}

func TestBlockedStdoutDoesNotStarveOTLPDelivery(t *testing.T) {
	writer := &blockingWriter{entered: make(chan struct{}), release: make(chan struct{})}
	exporter := &recordingLogExporter{exported: make(chan struct{}, 2)}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	logger := NewLoggerWithOptions(writer, WithQueueCapacity(2))
	logger.otel = provider.Logger("test")

	logger.Log().Info().Msg("first").Send()
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("stdout writer was not entered")
	}
	select {
	case <-exporter.exported:
	case <-time.After(time.Second):
		t.Fatal("first OTLP record was not exported")
	}

	logger.Log().Info().Msg("second").Send()
	select {
	case <-exporter.exported:
	case <-time.After(time.Second):
		t.Fatal("blocked stdout starved the later OTLP record")
	}

	close(writer.release)
	flushLogger(t, logger)
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown test log provider: %v", err)
	}
}

func TestOTLPBackpressureDropsAtCountedEtoQueue(t *testing.T) {
	exporter := &blockingLogExporter{entered: make(chan struct{}), release: make(chan struct{})}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	logger := NewLoggerWithOptions(io.Discard, WithQueueCapacity(1))
	logger.otel = provider.Logger("test")

	logger.Log().Info().Msg("first").Send()
	select {
	case <-exporter.entered:
	case <-time.After(time.Second):
		t.Fatal("OTLP exporter was not entered")
	}
	logger.Log().Info().Msg("second").Send()
	logger.Log().Info().Msg("third must drop from OTLP queue").Send()
	if got := logger.stats.otelDrops.Load(); got != 1 {
		t.Fatalf("OTLP queue drops = %d, want 1", got)
	}

	close(exporter.release)
	flushLogger(t, logger)
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown test log provider: %v", err)
	}
}

func TestTelemetryEmitFailureMetricReportsAtomicCounters(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() { _ = provider.Shutdown(context.Background()) }()
	var stats failureCounters
	stats.stdoutDrops.Store(2)
	stats.otelDrops.Store(3)
	stats.stdoutWrites.Store(4)
	stats.exports.Store(5)
	stats.stdoutPanics.Store(6)
	stats.otelPanics.Store(7)

	registration, err := registerTelemetryFailureMetric(provider.Meter("test"), &stats)
	if err != nil {
		t.Fatalf("register failure metric: %v", err)
	}
	defer func() { _ = registration.Unregister() }()

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect failure metric: %v", err)
	}
	got := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != telemetryEmitFailureMetricName {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data type = %T, want int64 sum", candidate.Data)
			}
			for _, point := range sum.DataPoints {
				failureType, _ := point.Attributes.Value(attribute.Key("failure_type"))
				sink, _ := point.Attributes.Value(attribute.Key("sink"))
				got[failureType.AsString()+"/"+sink.AsString()] = point.Value
			}
		}
	}
	want := map[string]int64{
		"queue_drop/stdout": 2,
		"queue_drop/otlp":   3,
		"write/stdout":      4,
		"export/otlp":       5,
		"panic/stdout":      6,
		"panic/otlp":        7,
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("%s = %d, want %d (all=%v)", key, got[key], value, got)
		}
	}
}

func TestDefaultLoggerFailureTotalIsScrapeSafeAggregate(t *testing.T) {
	logger := NewLogger(io.Discard)
	logger.stats.dropped.Store(2)
	logger.stats.stdoutWrites.Store(3)
	logger.stats.exports.Store(5)
	logger.stats.panics.Store(7)
	restore := SetLoggerForTesting(logger)
	defer restore()

	if got := DefaultLoggerFailureTotal(); got != 17 {
		t.Fatalf("failure total = %d, want 17", got)
	}
}

func TestTelemetryRuntimeShutdownHonorsConcurrentShutdownDeadline(t *testing.T) {
	runtime := &telemetryRuntime{shutdownGate: make(chan struct{}, 1)}
	runtime.shutdownGate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if err := runtime.shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error = %v, want deadline exceeded", err)
	}
}

func TestStaleRuntimeCannotClearCurrentRuntime(t *testing.T) {
	oldRuntime := &telemetryRuntime{}
	newRuntime := &telemetryRuntime{}
	runtimeMu.Lock()
	previous := activeRuntime
	activeRuntime = newRuntime
	runtimeMu.Unlock()
	defer func() {
		runtimeMu.Lock()
		activeRuntime = previous
		runtimeMu.Unlock()
	}()

	clearTelemetryRuntime(oldRuntime)
	if got := currentTelemetryRuntime(); got != newRuntime {
		t.Fatalf("active runtime = %p, want current %p", got, newRuntime)
	}
}

func TestLoggerSendDoesNotBlockOnBlockedWriter(t *testing.T) {
	writer := &blockingWriter{entered: make(chan struct{}), release: make(chan struct{})}
	logger := NewLoggerWithOptions(writer, WithQueueCapacity(1))

	started := time.Now()
	logger.Log().Info().Msg("first").Send()
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("writer was not entered")
	}
	logger.Log().Info().Msg("second").Send()
	logger.Log().Info().Msg("third must be dropped").Send()
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("Send blocked for %s", elapsed)
	}
	if got := logger.stats.stdoutDrops.Load(); got != 1 {
		t.Fatalf("stdout queue drops = %d, want 1", got)
	}
	if got := logger.Failures().Dropped; got < 1 || got > 2 {
		t.Fatalf("dropped records = %d, want one or two records depending on independent OTLP scheduling", got)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := logger.Shutdown(shutdownCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want deadline exceeded", err)
	}
	close(writer.release)
	drainCtx, drainCancel := context.WithTimeout(context.Background(), time.Second)
	defer drainCancel()
	if err := logger.Shutdown(drainCtx); err != nil {
		t.Fatalf("Shutdown after writer release: %v", err)
	}
}

func TestLoggerShutdownDeadlineIncludesConcurrentFlushLock(t *testing.T) {
	writer := &blockingWriter{entered: make(chan struct{}), release: make(chan struct{})}
	logger := NewLoggerWithOptions(writer, WithQueueCapacity(1))
	logger.Log().Info().Msg("first").Send()
	<-writer.entered
	logger.Log().Info().Msg("second").Send()

	flushDone := make(chan error, 1)
	go func() { flushDone <- logger.Flush(context.Background()) }()
	lockDeadline := time.Now().Add(time.Second)
	for {
		if logger.mu.TryLock() {
			logger.mu.Unlock()
			if time.Now().After(lockDeadline) {
				t.Fatal("Flush did not acquire the logger read lock")
			}
			runtime.Gosched()
			continue
		}
		break
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := logger.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("Shutdown ignored deadline while Flush held the queue lock: %s", elapsed)
	}

	close(writer.release)
	select {
	case err := <-flushDone:
		if err != nil {
			t.Fatalf("Flush after writer release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Flush did not finish after writer release")
	}
	drainCtx, drainCancel := context.WithTimeout(context.Background(), time.Second)
	defer drainCancel()
	if err := logger.Shutdown(drainCtx); err != nil {
		t.Fatalf("final Shutdown: %v", err)
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type failingLogExporter struct{}

func (failingLogExporter) Export(context.Context, []sdklog.Record) error {
	return errors.New("export failed")
}

func (failingLogExporter) Shutdown(context.Context) error   { return nil }
func (failingLogExporter) ForceFlush(context.Context) error { return nil }

type recordingLogExporter struct {
	exported chan struct{}
}

type blockingLogExporter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (exporter *blockingLogExporter) Export(context.Context, []sdklog.Record) error {
	exporter.once.Do(func() { close(exporter.entered) })
	<-exporter.release
	return nil
}

func (*blockingLogExporter) Shutdown(context.Context) error   { return nil }
func (*blockingLogExporter) ForceFlush(context.Context) error { return nil }

func (exporter *recordingLogExporter) Export(context.Context, []sdklog.Record) error {
	exporter.exported <- struct{}{}
	return nil
}

func (*recordingLogExporter) Shutdown(context.Context) error   { return nil }
func (*recordingLogExporter) ForceFlush(context.Context) error { return nil }

type blockingWriter struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (w *blockingWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(data), nil
}

func flushLogger(t *testing.T, logger *Logger) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := logger.Flush(ctx); err != nil {
		t.Fatalf("flush logger: %v", err)
	}
}

func decodeLogRecord(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &record); err != nil {
		t.Fatalf("decode log: %v\n%s", err, data)
	}
	return record
}

func assertLogValue(t *testing.T, record map[string]any, key string, want any) {
	t.Helper()
	if got := record[key]; got != want {
		t.Fatalf("%s = %#v, want %#v", key, got, want)
	}
}
