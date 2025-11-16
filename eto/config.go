package eto

type Config struct {
	ServiceName   string // ชื่อ service เช่น "service-a"
	Environment   string // dev / uat / prod
	OtelEndpoint  string // OTLP gRPC endpoint เช่น "otel-collector:4317"
	EnableMetrics bool   // เผื่ออนาคต
}
