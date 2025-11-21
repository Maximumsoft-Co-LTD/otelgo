package tracer

import (
	"context"
	"fmt"

	"github.com/Maximumsoft-Co-LTD/otelgo/eto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Start starts a new span with the given name and context.
// Returns the new context and a cleanup function that ends the span.
// Usage:
//
//	ctx, end := trace.Start(ctx, "operation-name")
//	defer end()
//
// Or with attributes:
//
//	ctx, end := trace.Start(ctx, "operation-name", "key1", "value1", "key2", 123)
//	defer end()
func Start(ctx context.Context, name string, attrs ...any) (context.Context, func()) {
	builder := eto.Trace().
		Name(name).
		FromContext(ctx)

	for i := 0; i < len(attrs)-1; i += 2 {
		if key, ok := attrs[i].(string); ok {
			builder = builder.Attr(key, attrs[i+1])
		}
	}

	ctx, span := builder.Start()
	return ctx, func() { span.End() }
}

// Run executes a function within a span, automatically handling errors.

func Run(ctx context.Context, name string, fn func(ctx context.Context) error, attrs ...any) error {
	builder := eto.Trace().
		Name(name).
		FromContext(ctx)

	for i := 0; i < len(attrs)-1; i += 2 {
		if key, ok := attrs[i].(string); ok {
			builder = builder.Attr(key, attrs[i+1])
		}
	}

	return builder.Run(fn)
}

// StartServer starts a server span (for HTTP handlers, gRPC servers, etc.).
// Usage:
//
//	ctx, end := trace.StartServer(ctx, "/api/users")
//	defer end()
//
// Or with attributes:
//
//	ctx, end := trace.StartServer(ctx, "/api/users", "http.method", "GET", "http.route", "/api/users")
//	defer end()
func StartServer(ctx context.Context, name string, attrs ...any) (context.Context, func()) {
	builder := eto.Trace().
		Name(name).
		FromContext(ctx).
		Kind(trace.SpanKindServer)

	for i := 0; i < len(attrs)-1; i += 2 {
		if key, ok := attrs[i].(string); ok {
			builder = builder.Attr(key, attrs[i+1])
		}
	}

	ctx, span := builder.Start()
	return ctx, func() { span.End() }
}

// StartClient starts a client span (for HTTP clients, gRPC clients, etc.).
// Usage:
//
//	ctx, end := trace.StartClient(ctx, "http.request")
//	defer end()
//
// Or with attributes:
//
//	ctx, end := trace.StartClient(ctx, "http.request", "http.method", "GET", "http.url", "https://example.com")
//	defer end()
func StartClient(ctx context.Context, name string, attrs ...any) (context.Context, func()) {
	builder := eto.Trace().
		Name(name).
		FromContext(ctx).
		Kind(trace.SpanKindClient)

	for i := 0; i < len(attrs)-1; i += 2 {
		if key, ok := attrs[i].(string); ok {
			builder = builder.Attr(key, attrs[i+1])
		}
	}

	ctx, span := builder.Start()
	return ctx, func() { span.End() }
}

// StartConsumer starts a consumer span (for message queue consumers).
// Usage:
//
//	ctx, end := trace.StartConsumer(ctx, "amqp.consume")
//	defer end()
//
// Or with attributes:
//
//	ctx, end := trace.StartConsumer(ctx, "amqp.consume", "amqp.queue", "my-queue", "amqp.exchange", "my-exchange")
//	defer end()
func StartConsumer(ctx context.Context, name string, attrs ...any) (context.Context, func()) {
	builder := eto.Trace().
		Name(name).
		FromContext(ctx).
		Kind(trace.SpanKindConsumer)

	for i := 0; i < len(attrs)-1; i += 2 {
		if key, ok := attrs[i].(string); ok {
			builder = builder.Attr(key, attrs[i+1])
		}
	}

	ctx, span := builder.Start()
	return ctx, func() { span.End() }
}

// StartProducer starts a producer span (for message queue producers).
// Usage:
//
//	ctx, end := trace.StartProducer(ctx, "amqp.publish")
//	defer end()
//
// Or with attributes:
//
//	ctx, end := trace.StartProducer(ctx, "amqp.publish", "amqp.queue", "my-queue", "amqp.exchange", "my-exchange")
//	defer end()
func StartProducer(ctx context.Context, name string, attrs ...any) (context.Context, func()) {
	builder := eto.Trace().
		Name(name).
		FromContext(ctx).
		Kind(trace.SpanKindProducer)

	for i := 0; i < len(attrs)-1; i += 2 {
		if key, ok := attrs[i].(string); ok {
			builder = builder.Attr(key, attrs[i+1])
		}
	}

	ctx, span := builder.Start()
	return ctx, func() { span.End() }
}

// Builder returns the underlying eto.Trace() builder for advanced usage.
// This allows you to use the full builder API when needed.
// Usage:
//
//	ctx, span := trace.Builder().
//	    Name("custom-operation").
//	    FromContext(ctx).
//	    Kind(trace.SpanKindInternal).
//	    Attr("custom", "value").
//	    Start()
//	defer span.End()
func Builder() *eto.TraceBuilder {
	return eto.Trace()
}

// Attr is a convenience function to create an attribute.
// It's a wrapper around eto.Trace().Attr() for consistency.
func Attr(key string, val any) attribute.KeyValue {
	switch v := val.(type) {
	case string:
		return attribute.String(key, v)
	case int:
		return attribute.Int(key, v)
	case int64:
		return attribute.Int64(key, v)
	case float64:
		return attribute.Float64(key, v)
	case bool:
		return attribute.Bool(key, v)
	default:
		return attribute.String(key, fmt.Sprintf("%v", v))
	}
}
