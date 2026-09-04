package eto

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"go.opentelemetry.io/otel"
	otlploggrpc "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	otlpmetricgrpc "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	otlpgrpc "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	logglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

var (
	initMu sync.Mutex

	runtimeMu        sync.RWMutex
	globalCfg        Config
	globalPropagator propagation.TextMapPropagator
	globalMeter      metric.Meter
	activeRuntime    *telemetryRuntime
)

type telemetryRuntime struct {
	logger              *Logger
	traceProvider       *sdktrace.TracerProvider
	meterProvider       *sdkmetric.MeterProvider
	logProvider         *sdklog.LoggerProvider
	failureRegistration metric.Registration
	shutdownGate        chan struct{}
	stopped             bool
}

// Init replaces any active eto runtime in a serialized lifecycle. A previous
// runtime must shut down successfully before a replacement is published, and
// every returned shutdown closure owns exactly the resources created here.
func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	initMu.Lock()
	defer initMu.Unlock()

	previous := currentTelemetryRuntime()
	if previous != nil {
		if err := previous.shutdown(ctx); err != nil {
			return nil, fmt.Errorf("shutdown previous eto runtime: %w", err)
		}
		clearTelemetryRuntime(previous)
	}

	runtime, propagator, err := buildTelemetryRuntime(ctx, cfg)
	if err != nil {
		return nil, err
	}
	publishTelemetryRuntime(cfg, propagator, runtime)

	shutdown := func(ctx context.Context) error {
		err := runtime.shutdown(ctx)
		if err == nil {
			clearTelemetryRuntime(runtime)
		}
		return err
	}
	return shutdown, nil
}

func buildTelemetryRuntime(ctx context.Context, cfg Config) (*telemetryRuntime, propagation.TextMapPropagator, error) {
	res, err := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.DeploymentEnvironment(cfg.Environment),
		),
	)
	if err != nil {
		return nil, nil, err
	}

	traceExp, err := otlpgrpc.New(
		ctx,
		otlpgrpc.WithEndpoint(cfg.OtelEndpoint),
		otlpgrpc.WithInsecure(),
	)
	if err != nil {
		return nil, nil, err
	}
	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)

	logger := NewLogger(os.Stdout)
	var meterProvider *sdkmetric.MeterProvider
	var failureRegistration metric.Registration
	if cfg.EnableMetrics {
		metricExp, err := otlpmetricgrpc.New(
			ctx,
			otlpmetricgrpc.WithEndpoint(cfg.OtelEndpoint),
			otlpmetricgrpc.WithInsecure(),
		)
		if err != nil {
			_ = logger.Shutdown(context.Background())
			_ = traceProvider.Shutdown(ctx)
			return nil, nil, err
		}
		meterProvider = sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
			sdkmetric.WithResource(res),
		)
		failureRegistration, err = registerTelemetryFailureMetric(meterProvider.Meter("eto"), &logger.stats)
		if err != nil {
			_ = logger.Shutdown(context.Background())
			_ = meterProvider.Shutdown(ctx)
			_ = traceProvider.Shutdown(ctx)
			return nil, nil, err
		}
	}

	rawLogExporter, err := otlploggrpc.New(
		ctx,
		otlploggrpc.WithEndpoint(cfg.OtelEndpoint),
		otlploggrpc.WithInsecure(),
	)
	if err != nil {
		if failureRegistration != nil {
			_ = failureRegistration.Unregister()
		}
		_ = logger.Shutdown(context.Background())
		if meterProvider != nil {
			_ = meterProvider.Shutdown(ctx)
		}
		_ = traceProvider.Shutdown(ctx)
		return nil, nil, err
	}
	logExporter := &failureCountingLogExporter{Exporter: rawLogExporter, stats: &logger.stats}
	logProvider := sdklog.NewLoggerProvider(
		// eto already owns the bounded asynchronous OTLP queue. A synchronous
		// SDK processor keeps that queue as the single, counted overload
		// boundary instead of silently dropping again inside an SDK batch queue.
		sdklog.WithProcessor(sdklog.NewSimpleProcessor(logExporter)),
		sdklog.WithResource(res),
	)
	logger.otel = logProvider.Logger("eto")

	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	return &telemetryRuntime{
		logger:              logger,
		traceProvider:       traceProvider,
		meterProvider:       meterProvider,
		logProvider:         logProvider,
		failureRegistration: failureRegistration,
		shutdownGate:        make(chan struct{}, 1),
	}, propagator, nil
}

func publishTelemetryRuntime(cfg Config, propagator propagation.TextMapPropagator, runtime *telemetryRuntime) {
	runtimeMu.Lock()
	resetMetricCaches()
	globalCfg = cfg
	if runtime.meterProvider != nil {
		globalMeter = runtime.meterProvider.Meter("eto")
	} else {
		globalMeter = nil
	}
	globalPropagator = propagator
	activeRuntime = runtime
	runtimeMu.Unlock()

	otel.SetTracerProvider(runtime.traceProvider)
	if runtime.meterProvider != nil {
		otel.SetMeterProvider(runtime.meterProvider)
	}
	logglobal.SetLoggerProvider(runtime.logProvider)
	otel.SetTextMapPropagator(propagator)
	setDefaultLogger(runtime.logger)
}

func currentTelemetryRuntime() *telemetryRuntime {
	runtimeMu.RLock()
	runtime := activeRuntime
	runtimeMu.RUnlock()
	return runtime
}

func clearTelemetryRuntime(runtime *telemetryRuntime) {
	runtimeMu.Lock()
	if activeRuntime == runtime {
		resetMetricCaches()
		activeRuntime = nil
		globalCfg = Config{}
		globalMeter = nil
		globalPropagator = nil
		setDefaultLogger(nil)
	}
	runtimeMu.Unlock()
}

func propagatorSnapshot() propagation.TextMapPropagator {
	runtimeMu.RLock()
	propagator := globalPropagator
	runtimeMu.RUnlock()
	return propagator
}

func (runtime *telemetryRuntime) shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case runtime.shutdownGate <- struct{}{}:
		defer func() { <-runtime.shutdownGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if runtime.stopped {
		return nil
	}

	var shutdownErrors []error
	if runtime.logger != nil {
		if err := runtime.logger.Shutdown(ctx); err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
	}
	if runtime.logProvider != nil {
		if err := runtime.logProvider.Shutdown(ctx); err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
	}
	if runtime.traceProvider != nil {
		if err := runtime.traceProvider.Shutdown(ctx); err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
	}
	// Keep the callback registered through the provider's final collection so
	// export failures observed above are included in the last health metric.
	if runtime.meterProvider != nil {
		if err := runtime.meterProvider.Shutdown(ctx); err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
	}
	if len(shutdownErrors) == 0 {
		runtime.stopped = true
	}
	return errors.Join(shutdownErrors...)
}

type failureCountingLogExporter struct {
	sdklog.Exporter
	stats *failureCounters
}

func (exporter *failureCountingLogExporter) Export(ctx context.Context, records []sdklog.Record) error {
	err := exporter.Exporter.Export(ctx, records)
	exporter.count(err)
	return err
}

func (exporter *failureCountingLogExporter) ForceFlush(ctx context.Context) error {
	err := exporter.Exporter.ForceFlush(ctx)
	exporter.count(err)
	return err
}

func (exporter *failureCountingLogExporter) Shutdown(ctx context.Context) error {
	err := exporter.Exporter.Shutdown(ctx)
	exporter.count(err)
	return err
}

func (exporter *failureCountingLogExporter) count(err error) {
	if err != nil && exporter.stats != nil {
		exporter.stats.exports.Add(1)
	}
}
