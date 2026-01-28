package main

import (
	"context"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/Maximumsoft-Co-LTD/otelgo/eto"
	"github.com/Maximumsoft-Co-LTD/otelgo/public/logger"
	"github.com/Maximumsoft-Co-LTD/otelgo/public/tracer"
	"github.com/gin-gonic/gin"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Initialize otelgo
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

	// Use the built-in GinMiddleware with options
	r.Use(tracer.GinMiddleware(
		tracer.WithServiceName("example-http-gin"),
		tracer.WithSkipPaths("/health", "/metrics"),
	))

	// Routes
	r.GET("/hello", helloHandler)
	r.GET("/users/:id", getUserHandler)
	r.POST("/users", createUserHandler)
	r.GET("/health", healthHandler)

	log.Println("http-gin example listening on :8091")
	if err := r.Run(":8091"); err != nil {
		log.Fatalf("gin run error: %v", err)
	}
}

// helloHandler demonstrates basic usage with child spans
func helloHandler(c *gin.Context) {
	ctx := c.Request.Context()

	// Log with trace context (trace_id and span_id auto-attached)
	logger.Info(ctx, "hello endpoint called",
		"client_ip", c.ClientIP(),
		"method", c.Request.Method,
	)

	// Create a child span for business logic
	tracer.Run(ctx, "hello.process",
		func(ctx context.Context) error {
			// Simulate some work
			time.Sleep(10 * time.Millisecond)

			logger.Info(ctx, "processing hello request")

			return nil
		},
		"custom_attr", "value1",
	)

	// Another child span example
	processData(ctx)

	c.JSON(http.StatusOK, gin.H{
		"message": "hello from http-gin example",
	})
}

// getUserHandler demonstrates span with parameters
func getUserHandler(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.Param("id")

	// Create span with user context
	ctx, end := tracer.Start(ctx, "get-user",
		"user.id", userID,
	)
	defer end()

	// Simulate database call
	user, err := fetchUserFromDB(ctx, userID)
	if err != nil {
		logger.Error(ctx, "failed to fetch user",
			"user_id", userID,
			"error", err.Error(),
		)
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	logger.Info(ctx, "user fetched successfully", "user_id", userID)

	c.JSON(http.StatusOK, user)
}

// createUserHandler demonstrates POST request handling
func createUserHandler(c *gin.Context) {
	ctx := c.Request.Context()

	var req struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn(ctx, "invalid request body", "error", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Create span for user creation
	err := tracer.Run(ctx, "create-user",
		func(ctx context.Context) error {
			// Simulate user creation
			time.Sleep(20 * time.Millisecond)

			logger.Info(ctx, "user created",
				"name", req.Name,
				"email", req.Email,
			)

			return nil
		},
		"user.name", req.Name,
		"user.email", req.Email,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "user created",
		"name":    req.Name,
		"email":   req.Email,
	})
}

// healthHandler - skipped by middleware (no tracing)
func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// processData demonstrates nested spans
func processData(ctx context.Context) {
	ctx, end := tracer.Start(ctx, "process-data")
	defer end()

	// Simulate processing with random delay
	delay := rand.Intn(50) + 10
	time.Sleep(time.Duration(delay) * time.Millisecond)

	// Nested operation
	validateData(ctx)
}

// validateData demonstrates deeply nested spans
func validateData(ctx context.Context) {
	ctx, end := tracer.Start(ctx, "validate-data")
	defer end()

	time.Sleep(5 * time.Millisecond)

	logger.Debug(ctx, "data validated")
}

// fetchUserFromDB simulates a database call with tracing
func fetchUserFromDB(ctx context.Context, userID string) (map[string]any, error) {
	ctx, end := tracer.Start(ctx, "db.query",
		"db.system", "postgresql",
		"db.operation", "SELECT",
		"db.table", "users",
	)
	defer end()

	// Simulate database latency
	delay := rand.Intn(30) + 10
	time.Sleep(time.Duration(delay) * time.Millisecond)

	logger.Debug(ctx, "database query executed",
		"query", "SELECT * FROM users WHERE id = ?",
		"latency_ms", delay,
	)

	return map[string]any{
		"id":    userID,
		"name":  "John Doe",
		"email": "john@example.com",
	}, nil
}
