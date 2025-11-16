package eto

import (
	"context"
	"time"
	
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/trace"
)

// AMQPConsumeHandler รูปแบบ handler ที่รับ ctx + message
type AMQPConsumeHandler func(ctx context.Context, msg amqp.Delivery) error

// AMQPConsumerInterceptor: wrap handler ให้มี span + metrics อัตโนมัติ
// ใช้ตอน consume: go func() { for msg := range msgs { wrapper(msg) } }()
func AMQPConsumerInterceptor(serviceName string, handler AMQPConsumeHandler) func(msg amqp.Delivery) {
	return func(msg amqp.Delivery) {
		// start จาก base context (จริง ๆ จะผูกกับ ctx global ของ service ก็ได้)
		baseCtx := context.Background()

		// Extract trace จาก message headers
		ctx := Propagate().
			FromContext(baseCtx).
			FromAMQP(msg.Headers)

		// เริ่ม span consumer
		_ = Trace().
			Name("amqp.consume").
			FromContext(ctx).
			Kind(trace.SpanKindConsumer).
			Attr("amqp.queue", msg.RoutingKey).
			Attr("amqp.exchange", msg.Exchange).
			Run(func(ctx context.Context) error {
				start := time.Now()

				err := handler(ctx, msg)

				// metrics: นับ consume + latency
				status := "success"
				if err != nil {
					status = "error"
				}

				MetricCounter("amqp_consume_total").
					Attr("service", serviceName).
					Attr("queue", msg.RoutingKey).
					Attr("status", status).
					Add(ctx, 1)

				latencyMs := float64(time.Since(start).Milliseconds())
				MetricHistogram("amqp_consume_duration_ms").
					Attr("service", serviceName).
					Attr("queue", msg.RoutingKey).
					Attr("status", status).
					Record(ctx, latencyMs)

				return err
			})
	}
}
