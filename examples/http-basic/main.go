package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Maximumsoft-Co-LTD/otelgo/eto"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	shutdown, err := eto.Init(ctx, eto.Config{
		ServiceName:  "example-http-basic",
		Environment:  "dev",
		OtelEndpoint: "otel-collector:4317",
	})
	if err != nil {
		log.Fatalf("eto init error: %v", err)
	}
	defer shutdown(context.Background())

	mux := http.NewServeMux()
	mux.Handle("/hello", otelMiddleware(http.HandlerFunc(helloHandler)))

	log.Println("http-basic example listening on :8090")
	if err := http.ListenAndServe(":8090", mux); err != nil {
		log.Fatalf("ListenAndServe error: %v", err)
	}
}

// otelMiddleware: ดึง trace จาก header + สร้าง server span + ใส่ trace_id คืนใน response
func otelMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract trace จาก header (traceparent/baggage/x-trace-id ฯลฯ)
		ctx := eto.Propagate().FromHTTPRequest(r)

		// Start server span
		ctx, span := eto.Trace().
			Name(r.URL.Path).
			FromContext(ctx).
			Kind(trace.SpanKindServer).
			Attr("http.method", r.Method).
			Attr("http.route", r.URL.Path).
			Start()
		defer span.End()

		// inject ctx กลับเข้าไปใน request
		r = r.WithContext(ctx)

		// ส่งต่อให้ handler
		next.ServeHTTP(w, r)

		// set response header x-trace-id/x-span-id ให้ client
		eto.Propagate().
			FromContext(ctx).
			ToHTTPResponse(w)
	})
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := time.Now()

	_ = eto.Trace().
		Name("http-basic.hello").
		FromContext(ctx).
		Run(func(ctx context.Context) error {
			// metrics: นับ request
			eto.MetricCounter("http_requests_total").
				Attr("service", "example-http-basic").
				Attr("route", "/hello").
				Attr("method", r.Method).
				Add(ctx, 1)

			eto.Log().
				FromContext(ctx).
				Info().
				Msg("hello called").
				Field("remote_addr", r.RemoteAddr).
				Send()

			fmt.Fprintln(w, "hello from http-basic example")

			// metrics: latency
			latencyMs := float64(time.Since(start).Milliseconds())
			eto.MetricHistogram("http_request_duration_ms").
				Attr("service", "example-http-basic").
				Attr("route", "/hello").
				Attr("method", r.Method).
				Record(ctx, latencyMs)

			return nil
		})
}
