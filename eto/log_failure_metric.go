package eto

import (
	"context"
	"math"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const telemetryEmitFailureMetricName = "telemetry_emit_failure"

func registerTelemetryFailureMetric(meter metric.Meter, stats *failureCounters) (metric.Registration, error) {
	counter, err := meter.Int64ObservableCounter(
		telemetryEmitFailureMetricName,
		metric.WithUnit("1"),
		metric.WithDescription("Cumulative failures while delivering structured telemetry logs."),
	)
	if err != nil {
		return nil, err
	}

	return meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		observations := []struct {
			value       uint64
			failureType string
			sink        string
		}{
			{stats.stdoutDrops.Load(), "queue_drop", "stdout"},
			{stats.otelDrops.Load(), "queue_drop", "otlp"},
			{stats.stdoutWrites.Load(), "write", "stdout"},
			{stats.exports.Load(), "export", "otlp"},
			{stats.stdoutPanics.Load(), "panic", "stdout"},
			{stats.otelPanics.Load(), "panic", "otlp"},
		}
		for _, observation := range observations {
			observer.ObserveInt64(
				counter,
				boundedInt64(observation.value),
				metric.WithAttributes(
					attribute.String("failure_type", observation.failureType),
					attribute.String("sink", observation.sink),
				),
			)
		}
		return nil
	}, counter)
}

func boundedInt64(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}
