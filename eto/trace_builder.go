package eto

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type TraceBuilder struct {
	name       string
	ctx        context.Context
	attrs      []attribute.KeyValue
	kind       trace.SpanKind
	recordErr  bool
	setStatus  bool
	tracerName string
}

type SpanScope struct {
	ctx  context.Context
	span trace.Span
}

func (s *SpanScope) Ctx() context.Context {
	if s == nil || s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

func (s *SpanScope) Span() trace.Span {
	return s.span
}

func (s *SpanScope) Done() {
	if s != nil && s.span != nil {
		s.span.End()
	}
}

func Trace() *TraceBuilder {
	return &TraceBuilder{
		ctx:        context.Background(),
		kind:       trace.SpanKindInternal,
		recordErr:  true,
		setStatus:  true,
		tracerName: "eto",
	}
}

func (b *TraceBuilder) Name(name string) *TraceBuilder {
	b.name = name
	return b
}

func (b *TraceBuilder) FromContext(ctx context.Context) *TraceBuilder {
	if ctx != nil {
		b.ctx = ctx
	}
	return b
}

func (b *TraceBuilder) Kind(kind trace.SpanKind) *TraceBuilder {
	b.kind = kind
	return b
}

func (b *TraceBuilder) TracerName(name string) *TraceBuilder {
	if name != "" {
		b.tracerName = name
	}
	return b
}

func (b *TraceBuilder) Attr(key string, val any) *TraceBuilder {
	switch v := val.(type) {
	case string:
		b.attrs = append(b.attrs, attribute.String(key, v))
	case int:
		b.attrs = append(b.attrs, attribute.Int(key, v))
	case int64:
		b.attrs = append(b.attrs, attribute.Int64(key, v))
	case float64:
		b.attrs = append(b.attrs, attribute.Float64(key, v))
	case bool:
		b.attrs = append(b.attrs, attribute.Bool(key, v))
	default:
		b.attrs = append(b.attrs, attribute.String(key, fmt.Sprintf("%v", v)))
	}
	return b
}

func (b *TraceBuilder) Attrs(attrs ...attribute.KeyValue) *TraceBuilder {
	b.attrs = append(b.attrs, attrs...)
	return b
}

func (b *TraceBuilder) RecordError(enable bool) *TraceBuilder {
	b.recordErr = enable
	return b
}

func (b *TraceBuilder) SetStatusOnError(enable bool) *TraceBuilder {
	b.setStatus = enable
	return b
}

func (b *TraceBuilder) Start() (context.Context, trace.Span) {
	if b.name == "" {
		b.name = "unnamed-span"
	}
	tr := otel.Tracer(b.tracerName)
	ctx, span := tr.Start(b.ctx, b.name, trace.WithSpanKind(b.kind))
	if len(b.attrs) > 0 {
		span.SetAttributes(b.attrs...)
	}
	return ctx, span
}

func (b *TraceBuilder) StartScope() *SpanScope {
	ctx, span := b.Start()
	return &SpanScope{
		ctx:  ctx,
		span: span,
	}
}

func (b *TraceBuilder) Run(fn func(ctx context.Context) error) error {
	if fn == nil {
		return errors.New("eto.Trace().Run: fn is nil")
	}

	ctx, span := b.Start()
	defer span.End()

	err := fn(ctx)
	if err != nil {
		if b.recordErr {
			span.RecordError(err)
		}
		if b.setStatus {
			span.SetStatus(codes.Error, err.Error())
		}
	}
	return err
}
