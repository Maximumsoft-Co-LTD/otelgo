package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/Maximumsoft-Co-LTD/otelgo/eto"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	shutdown, err := eto.Init(ctx, eto.Config{
		ServiceName:  "example-http-gin",
		Environment:  "dev",
		OtelEndpoint: "otel-collector:4317",
	})
	if err != nil {
		log.Fatalf("eto init error: %v", err)
	}
	defer shutdown(context.Background())

	r := gin.Default()
	r.Use(otelGinMiddleware())

	r.GET("/hello", helloGin)

	log.Println("http-gin example listening on :8091")
	if err := r.Run(":8091"); err != nil {
		log.Fatalf("gin run error: %v", err)
	}
}

func otelGinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := eto.Propagate().FromHTTPRequest(c.Request)

		ctx, span := eto.Trace().
			Name(c.FullPath()).
			FromContext(ctx).
			Kind(trace.SpanKindServer).
			Attr("http.method", c.Request.Method).
			Attr("http.route", c.FullPath()).
			Start()
		defer span.End()

		c.Request = c.Request.WithContext(ctx)

		c.Next()

		eto.Propagate().
			FromContext(ctx).
			ToHTTPResponse(c.Writer)
	}
}

func helloGin(c *gin.Context) {
	ctx := c.Request.Context()
	start := time.Now()

	eto.Log().
		FromContext(ctx).
		Info().
		Msg("gin hello 1").
		Field("client_ip", c.ClientIP()).
		Send()

	_ = eto.Trace().
		Name("gin.hello").
		FromContext(ctx).
		Attr("custom attr 1", "test1").
		Attr("custom attr 2", "test2").
		Run(func(ctx context.Context) error {
			eto.MetricCounter("http_requests_total").
				Attr("service", "example-http-gin").
				Attr("route", "/hello").
				Attr("method", c.Request.Method).
				Add(ctx, 1)

			eto.Log().
				FromContext(ctx).
				Info().
				Msg("gin hello 2").
				Field("client_ip", c.ClientIP()).
				Send()

			c.JSON(http.StatusOK, gin.H{"message": "hello from http-gin example"})

			latencyMs := float64(time.Since(start).Milliseconds())
			eto.MetricHistogram("http_request_duration_ms").
				Attr("service", "example-http-gin").
				Attr("route", "/hello").
				Attr("method", c.Request.Method).
				Record(ctx, latencyMs)

			return nil
		})
}
