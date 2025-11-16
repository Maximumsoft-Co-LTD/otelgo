package eto

import (
	"context"
	"net/http"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
)

type PropagationBuilder struct {
	ctx       context.Context
	useLegacy bool
	err       interface{}
}

// Propagate เริ่ม Fluent builder สำหรับ Inject/Extract
func Propagate() *PropagationBuilder {
	return &PropagationBuilder{
		ctx: context.Background(),
	}
}

func (p *PropagationBuilder) FromContext(ctx context.Context) *PropagationBuilder {
	if ctx != nil {
		p.ctx = ctx
	}
	return p
}

func (p *PropagationBuilder) WithLegacyHeaders(enable bool) *PropagationBuilder {
	p.useLegacy = enable
	return p
}

// ---------- HTTP Inbound ----------

func (p *PropagationBuilder) FromHTTPRequest(r *http.Request) context.Context {
	if globalPropagator == nil {
		return r.Context()
	}
	return globalPropagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
}

// ---------- HTTP Outbound ----------

func (p *PropagationBuilder) ToHTTPRequest(r *http.Request) {
	if globalPropagator == nil {
		return
	}
	globalPropagator.Inject(p.ctx, propagation.HeaderCarrier(r.Header))

	if !p.useLegacy {
		return
	}

	span := trace.SpanFromContext(p.ctx)
	if span == nil {
		return
	}
	sc := span.SpanContext()
	if !sc.IsValid() {
		return
	}

	r.Header.Set("x-trace-id", sc.TraceID().String())
	r.Header.Set("x-span-id", sc.SpanID().String())
}

// ---------- HTTP Response ----------

func (p *PropagationBuilder) ToHTTPResponse(w http.ResponseWriter) {
	span := trace.SpanFromContext(p.ctx)
	if span == nil {
		return
	}
	sc := span.SpanContext()
	if !sc.IsValid() {
		return
	}
	w.Header().Set("x-trace-id", sc.TraceID().String())
	w.Header().Set("x-span-id", sc.SpanID().String())
}

// ---------- gRPC (optional) ----------

type metadataCarrier struct {
	metadata.MD
}

func (c metadataCarrier) Get(key string) string {
	vals := c.MD.Get(key)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

func (c metadataCarrier) Set(key, val string) {
	c.MD.Set(key, val)
}

func (c metadataCarrier) Keys() []string {
	out := make([]string, 0, len(c.MD))
	for k := range c.MD {
		out = append(out, k)
	}
	return out
}

func (p *PropagationBuilder) FromGRPCMetadata(ctx context.Context, md metadata.MD) context.Context {
	if globalPropagator == nil {
		return ctx
	}
	carrier := metadataCarrier{md}
	return globalPropagator.Extract(ctx, carrier)
}

func (p *PropagationBuilder) ToGRPCMetadata(ctx context.Context, md *metadata.MD) {
	if globalPropagator == nil {
		return
	}
	if md == nil {
		*md = metadata.MD{}
	}
	carrier := metadataCarrier{*md}
	globalPropagator.Inject(ctx, carrier)
}

// ---------- AMQP (RabbitMQ) ----------

// amqpHeaderCarrier ทำให้ amqp.Table ใช้กับ propagator ได้
type amqpHeaderCarrier amqp.Table

func (c amqpHeaderCarrier) Get(key string) string {
	if v, ok := c[key]; ok {
		if s, ok2 := v.(string); ok2 {
			return s
		}
	}
	return ""
}

func (c amqpHeaderCarrier) Set(key, val string) {
	c[key] = val
}

func (c amqpHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// FromAMQP: ดึง trace context จาก headers ของ AMQP message
// ใช้แบบ: ctx := eto.Propagate().FromContext(baseCtx).FromAMQP(msg.Headers)
func (p *PropagationBuilder) FromAMQP(headers amqp.Table) context.Context {
	if globalPropagator == nil {
		return p.ctx
	}
	carrier := amqpHeaderCarrier(headers)
	return globalPropagator.Extract(p.ctx, carrier)
}

// ToAMQP: inject trace context ลง headers เวลาจะ publish
// ใช้แบบ: eto.Propagate().FromContext(ctx).WithLegacyHeaders(true).ToAMQP(headers)
func (p *PropagationBuilder) ToAMQP(headers amqp.Table) {
	if globalPropagator == nil {
		return
	}
	carrier := amqpHeaderCarrier(headers)
	globalPropagator.Inject(p.ctx, carrier)

	if !p.useLegacy {
		return
	}

	// เพิ่ม x-trace-id / x-span-id เป็น legacy header (optional)
	span := trace.SpanFromContext(p.ctx)
	if span == nil {
		return
	}
	sc := span.SpanContext()
	if !sc.IsValid() {
		return
	}

	headers["x-trace-id"] = sc.TraceID().String()
	headers["x-span-id"] = sc.SpanID().String()
}
