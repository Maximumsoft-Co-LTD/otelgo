package eto

import (
	"context"

	"go.opentelemetry.io/otel"
	otlpmetricgrpc "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

var (
	globalCfg        Config
	globalTP         *sdktrace.TracerProvider
	globalMP         *sdkmetric.MeterProvider
	globalLogger     *zap.Logger
	globalPropagator propagation.TextMapPropagator
	globalMeter      metric.Meter
)

func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	globalCfg = cfg

	// ===== Resource =====
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

	// ===== Trace Exporter (OTLP gRPC) =====
	traceExp, err := otlptracegrpc.New(
		ctx,
		otlptracegrpc.WithEndpoint(cfg.OtelEndpoint),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithDialOption(
			grpc.WithBlock(),
			//grpc.WithTransportCredentials(insecure.NewCredentials()),
		),
	)
	if err != nil {
		return nil, err
	}

	// ===== Metric Exporter (OTLP gRPC) =====
	metricExp, err := otlpmetricgrpc.New(
		ctx,
		otlpmetricgrpc.WithEndpoint(cfg.OtelEndpoint),
		otlpmetricgrpc.WithInsecure(),
		otlpmetricgrpc.WithDialOption(
			grpc.WithBlock(),
			//grpc.WithTransportCredentials(insecure.NewCredentials()),
		),
	)
	if err != nil {
		return nil, err
	}

	// ===== Tracer Provider =====
	globalTP = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(globalTP)

	// ===== Meter Provider =====
	reader := sdkmetric.NewPeriodicReader(metricExp) // ดึง metrics ไปส่งทุก ๆ interval
	globalMP = sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(globalMP)
	globalMeter = globalMP.Meter("eto")

	// ===== Propagator (W3C traceparent + baggage) =====
	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(propagator)
	globalPropagator = propagator

	// ===== Logger (Zap) =====
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, err
	}
	globalLogger = logger

	// ===== Shutdown =====
	shutdown := func(ctx context.Context) error {
		if globalTP != nil {
			_ = globalTP.Shutdown(ctx)
		}
		if globalMP != nil {
			_ = globalMP.Shutdown(ctx)
		}
		if globalLogger != nil {
			_ = globalLogger.Sync()
		}
		return nil
	}

	return shutdown, nil
}
