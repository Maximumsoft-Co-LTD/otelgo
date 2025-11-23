package main

import (
	"context"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/Maximumsoft-Co-LTD/otelgo/eto"
	"github.com/Maximumsoft-Co-LTD/otelgo/logger"
	"github.com/Maximumsoft-Co-LTD/otelgo/metricer"
	"github.com/Maximumsoft-Co-LTD/otelgo/tracer"
	"github.com/gin-gonic/gin"
)

func main() {

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	shutdown, err := eto.Init(ctx, eto.Config{
		ServiceName:   "example-http-gin",
		Environment:   "dev",
		OtelEndpoint:  "0.0.0.0:4317",
		EnableMetrics: true,
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

		ctx, end := tracer.Start(ctx, c.FullPath(),
			"http.method", c.Request.Method,
			"http.route", c.FullPath(),
		)
		defer end()

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

	logger.Info(ctx, "gin hello 1", "client_ip", c.ClientIP(), "method", c.Request.Method)

	metricer.Counter(ctx, "http_requests_total", 1,
		"service", "example-http-gin",
		"route", "/hello",
		"method", c.Request.Method,
	)

	tracer.Run(ctx, "gin.hello",
		func(ctx context.Context) error {
			logger.Info(ctx, "gin hello 2",
				"status", 200,
			)

			c.JSON(http.StatusOK, gin.H{"message": "hello from http-gin example"})

			latencyMs := float64(time.Since(start).Milliseconds())
			metricer.Histogram(ctx, "http_request_duration_ms", latencyMs,
				"service", "example-http-gin",
				"route", "/hello",
				"method", c.Request.Method,
			)
			return nil
		},
		"custom attr 1", "test1",
		"custom attr 2", "test2",
	)

	process1(ctx)
}

func process1(ctx context.Context) {
	ctx, end := tracer.Start(ctx, "process1")
	defer end()

	ran := rand.Intn(100)
	time.Sleep(time.Duration(ran) * time.Millisecond)
}
