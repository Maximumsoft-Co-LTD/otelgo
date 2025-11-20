package metricer

import (
	"context"

	"github.com/Maximumsoft-Co-LTD/otelgo/eto"
)

// Counter increments a counter metric with the given name and value.
// Attributes can be provided as key-value pairs.
// Usage:
//
//	metricer.Counter(ctx, "http_requests_total", 1, "service", "my-service", "route", "/hello")
//
// Or without attributes:
//
//	metricer.Counter(ctx, "http_requests_total", 1)
func Counter(ctx context.Context, name string, value int64, attrs ...any) {
	builder := eto.MetricCounter(name)

	for i := 0; i < len(attrs)-1; i += 2 {
		if key, ok := attrs[i].(string); ok {
			builder = builder.Attr(key, attrs[i+1])
		}
	}

	builder.Add(ctx, value)
}

// Histogram records a histogram metric with the given name and value.
// Attributes can be provided as key-value pairs.
// Usage:
//
//	latencyMs := float64(time.Since(start).Milliseconds())
//	metricer.Histogram(ctx, "http_request_duration_ms", latencyMs, "service", "my-service", "route", "/hello")
//
// Or without attributes:
//
//	metricer.Histogram(ctx, "http_request_duration_ms", latencyMs)
func Histogram(ctx context.Context, name string, value float64, attrs ...any) {
	builder := eto.MetricHistogram(name)

	for i := 0; i < len(attrs)-1; i += 2 {
		if key, ok := attrs[i].(string); ok {
			builder = builder.Attr(key, attrs[i+1])
		}
	}

	builder.Record(ctx, value)
}
