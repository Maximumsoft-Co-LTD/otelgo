package eto

import (
	"context"

	"go.opentelemetry.io/otel"
	otlploggrpc "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	otlpmetricgrpc "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	otlpgrpc "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otellog "go.opentelemetry.io/otel/log"
	logglobal "go.opentelemetry.io/otel/log/global"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"

	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

var (
	globalCfg         Config
	globalTP          *sdktrace.TracerProvider
	globalMP          *sdkmetric.MeterProvider
	globalLogProvider *sdklog.LoggerProvider
	globalOtelLogger  otellog.Logger
	globalLogger      *zap.Logger
	globalPropagator  propagation.TextMapPropagator
	globalMeter       metric.Meter
)

func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	globalCfg = cfg

	res, err := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.DeploymentEnvironment(cfg.Environment),
		),
	)
	if err != nil {
		return nil, err
	}

	traceExp, err := otlpgrpc.New(
		ctx,
		otlpgrpc.WithEndpoint(cfg.OtelEndpoint),
		otlpgrpc.WithInsecure(),
		otlpgrpc.WithDialOption(grpc.WithBlock()),
	)
	if err != nil {
		return nil, err
	}

	globalTP = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(globalTP)

	if cfg.EnableMetrics {
		metricExp, err := otlpmetricgrpc.New(
			ctx,
			otlpmetricgrpc.WithEndpoint(cfg.OtelEndpoint),
			otlpmetricgrpc.WithInsecure(),
			otlpmetricgrpc.WithDialOption(grpc.WithBlock()),
		)
		if err != nil {
			return nil, err
		}

		reader := sdkmetric.NewPeriodicReader(metricExp)
		globalMP = sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(reader),
			sdkmetric.WithResource(res),
		)
		otel.SetMeterProvider(globalMP)
		globalMeter = globalMP.Meter("eto")
	}

	logExp, err := otlploggrpc.New(
		ctx,
		otlploggrpc.WithEndpoint(cfg.OtelEndpoint),
		otlploggrpc.WithInsecure(),
		otlploggrpc.WithDialOption(grpc.WithBlock()),
	)
	if err != nil {
		return nil, err
	}

	globalLogProvider = sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		sdklog.WithResource(res),
	)
	logglobal.SetLoggerProvider(globalLogProvider)

	globalOtelLogger = globalLogProvider.Logger("eto")

	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(propagator)
	globalPropagator = propagator

	logger, err := zap.NewProduction()
	if err != nil {
		return nil, err
	}
	globalLogger = logger

	shutdown := func(ctx context.Context) error {
		if globalTP != nil {
			_ = globalTP.Shutdown(ctx)
		}
		if globalMP != nil {
			_ = globalMP.Shutdown(ctx)
		}
		if globalLogProvider != nil {
			_ = globalLogProvider.Shutdown(ctx)
		}
		if globalLogger != nil {
			_ = globalLogger.Sync()
		}
		return nil
	}

	return shutdown, nil
}
