package tracer

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Maximumsoft-Co-LTD/otelgo/eto"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// MiddlewareOption is a function that configures MiddlewareConfig.
type MiddlewareOption func(*MiddlewareConfig)

// MiddlewareConfig holds configuration for the Gin middleware.
type MiddlewareConfig struct {
	// TracerName is the name of the tracer (default: "gin-otel")
	TracerName string

	// ServiceName is the name of the service (optional, uses global if not set)
	ServiceName string

	// SkipPaths is a list of paths to skip tracing (e.g., "/health", "/metrics")
	SkipPaths []string

	// SpanNameFormatter formats the span name. Default uses the route path.
	// Receives method and path, returns span name.
	SpanNameFormatter func(method, path string) string

	// RecordRequestBody if true, records request body as span attribute (careful with sensitive data)
	RecordRequestBody bool

	// RecordResponseBody if true, records response body as span attribute (careful with sensitive data)
	RecordResponseBody bool

	// EnableMetrics if true, records HTTP metrics (counter and histogram)
	EnableMetrics bool

	// PropagateToResponse if true, adds trace headers to response
	PropagateToResponse bool
}

// WithTracerName sets the tracer name.
func WithTracerName(name string) MiddlewareOption {
	return func(c *MiddlewareConfig) {
		c.TracerName = name
	}
}

// WithServiceName sets the service name for metrics.
func WithServiceName(name string) MiddlewareOption {
	return func(c *MiddlewareConfig) {
		c.ServiceName = name
	}
}

// WithSkipPaths sets paths to skip tracing.
func WithSkipPaths(paths ...string) MiddlewareOption {
	return func(c *MiddlewareConfig) {
		c.SkipPaths = paths
	}
}

// WithSpanNameFormatter sets a custom span name formatter.
func WithSpanNameFormatter(fn func(method, path string) string) MiddlewareOption {
	return func(c *MiddlewareConfig) {
		c.SpanNameFormatter = fn
	}
}

// WithMetrics enables HTTP metrics collection.
func WithMetrics() MiddlewareOption {
	return func(c *MiddlewareConfig) {
		c.EnableMetrics = true
	}
}

// WithResponsePropagation enables trace header propagation to response.
func WithResponsePropagation() MiddlewareOption {
	return func(c *MiddlewareConfig) {
		c.PropagateToResponse = true
	}
}

// defaultConfig returns the default middleware configuration.
func defaultConfig() *MiddlewareConfig {
	return &MiddlewareConfig{
		TracerName:          "gin-otel",
		SkipPaths:           []string{},
		EnableMetrics:       true,
		PropagateToResponse: true,
		SpanNameFormatter: func(method, path string) string {
			if path == "" {
				path = "unknown"
			}
			return fmt.Sprintf("%s %s", method, path)
		},
	}
}

// GinMiddleware returns a Gin middleware that provides OpenTelemetry tracing.
//
// Usage:
//
//	r := gin.Default()
//	r.Use(tracer.GinMiddleware())
//
// With options:
//
//	r.Use(tracer.GinMiddleware(
//	    tracer.WithServiceName("my-service"),
//	    tracer.WithSkipPaths("/health", "/metrics"),
//	    tracer.WithMetrics(),
//	))
func GinMiddleware(opts ...MiddlewareOption) gin.HandlerFunc {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	// Build skip paths map for O(1) lookup
	skipPaths := make(map[string]bool, len(cfg.SkipPaths))
	for _, path := range cfg.SkipPaths {
		skipPaths[path] = true
	}

	return func(c *gin.Context) {
		// Skip tracing for configured paths
		if skipPaths[c.Request.URL.Path] || skipPaths[c.FullPath()] {
			c.Next()
			return
		}

		start := time.Now()

		// Extract trace context from incoming request headers
		ctx := eto.Propagate().FromHTTPRequest(c.Request)

		// Determine span name
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		spanName := cfg.SpanNameFormatter(c.Request.Method, path)

		// Start server span with HTTP semantic conventions
		builder := eto.Trace().
			Name(spanName).
			FromContext(ctx).
			Kind(trace.SpanKindServer).
			Attr("http.method", c.Request.Method).
			Attr("http.scheme", scheme(c.Request)).
			Attr("http.target", c.Request.URL.Path).
			Attr("http.route", path).
			Attr("http.user_agent", c.Request.UserAgent()).
			Attr("http.request_content_length", c.Request.ContentLength).
			Attr("net.host.name", c.Request.Host).
			Attr("net.peer.ip", c.ClientIP())

		// Add query string if present
		if c.Request.URL.RawQuery != "" {
			builder = builder.Attr("http.url", c.Request.URL.String())
		}

		// Add tracer name if configured
		if cfg.TracerName != "" {
			builder = builder.TracerName(cfg.TracerName)
		}

		ctx, span := builder.Start()
		defer span.End()

		// Update request context
		c.Request = c.Request.WithContext(ctx)

		// Process request
		c.Next()

		// Get response status
		status := c.Writer.Status()

		// Set response attributes
		span.SetAttributes(
			Attr("http.status_code", status),
			Attr("http.response_content_length", c.Writer.Size()),
		)

		// Set span status based on HTTP status code
		if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", status))
			// Record any errors that occurred
			if len(c.Errors) > 0 {
				for _, err := range c.Errors {
					span.RecordError(err.Err)
				}
			}
		} else if status >= http.StatusBadRequest {
			// 4xx errors are not server errors, but we can note them
			span.SetAttributes(Attr("http.error", true))
		} else {
			span.SetStatus(codes.Ok, "")
		}

		// Record metrics if enabled
		if cfg.EnableMetrics {
			attrs := []any{
				"method", c.Request.Method,
				"path", path,
				"status", status,
				"status_class", statusClass(status),
			}
			if cfg.ServiceName != "" {
				attrs = append(attrs, "service", cfg.ServiceName)
			}

			// Request counter
			counterBuilder := eto.MetricCounter("http_requests_total")
			for i := 0; i < len(attrs)-1; i += 2 {
				if key, ok := attrs[i].(string); ok {
					counterBuilder = counterBuilder.Attr(key, attrs[i+1])
				}
			}
			counterBuilder.Add(ctx, 1)

			// Request duration histogram
			latencyMs := float64(time.Since(start).Milliseconds())
			histBuilder := eto.MetricHistogram("http_request_duration_ms")
			for i := 0; i < len(attrs)-1; i += 2 {
				if key, ok := attrs[i].(string); ok {
					histBuilder = histBuilder.Attr(key, attrs[i+1])
				}
			}
			histBuilder.Record(ctx, latencyMs)

			// Response size histogram
			if c.Writer.Size() > 0 {
				sizeBuilder := eto.MetricHistogram("http_response_size_bytes").
					Attr("method", c.Request.Method).
					Attr("path", path)
				sizeBuilder.Record(ctx, float64(c.Writer.Size()))
			}
		}

		// Propagate trace context to response headers if enabled
		if cfg.PropagateToResponse {
			eto.Propagate().FromContext(ctx).ToHTTPResponse(c.Writer)
		}
	}
}

// scheme returns the HTTP scheme (http or https).
func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	// Check common proxy headers
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return "http"
}

// statusClass returns the HTTP status class (1xx, 2xx, 3xx, 4xx, 5xx).
func statusClass(status int) string {
	switch {
	case status >= 500:
		return "5xx"
	case status >= 400:
		return "4xx"
	case status >= 300:
		return "3xx"
	case status >= 200:
		return "2xx"
	default:
		return "1xx"
	}
}

// Propagate returns a new PropagationBuilder for trace context propagation.
// This is a convenience wrapper around eto.Propagate().
func Propagate() *eto.PropagationBuilder {
	return eto.Propagate()
}

// Trace returns a new TraceBuilder for creating spans.
// This is a convenience wrapper around eto.Trace().
func Trace() *eto.TraceBuilder {
	return eto.Trace()
}

// MetricCounter returns a new CounterBuilder for counter metrics.
// This is a convenience wrapper around eto.MetricCounter().
func MetricCounter(name string) *eto.CounterBuilder {
	return eto.MetricCounter(name)
}

// MetricHistogram returns a new HistogramBuilder for histogram metrics.
// This is a convenience wrapper around eto.MetricHistogram().
func MetricHistogram(name string) *eto.HistogramBuilder {
	return eto.MetricHistogram(name)
}
