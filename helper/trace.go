package helper

import (
	"context"
	"fmt"
	"reflect"

	"github.com/Maximumsoft-Co-LTD/otelgo/eto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type TraceStruct struct {
	Ctx               context.Context
	Name              string
	Trace             *eto.SpanScope
	TraceCtx          context.Context
	TraceSpan         trace.Span
	TraceSpanCtx      context.Context
	TraceChildSpan    trace.Span
	TraceChildSpanCtx context.Context
}

func TraceCtx(ctx context.Context, name string) TraceStruct {
	scope := eto.Trace().
		Name(name).
		FromContext(ctx).
		StartScope()

	return TraceStruct{
		Ctx:      ctx,
		Trace:    scope,
		TraceCtx: scope.Ctx(),
		Name:     name,
	}
}

// TraceClose closes the trace and all child spans
func (t *TraceStruct) TraceClose() {
	t.EndChild()
	t.EndSpan()

	if t.Trace != nil {
		t.Trace.Done()
	}
}

// Span creates a new span under the trace
func (t *TraceStruct) Span(name string) {
	if t.TraceCtx == nil {
		return
	}

	t.TraceSpanCtx, t.TraceSpan = eto.Trace().
		Name(name).
		FromContext(t.TraceCtx).
		Start()
}

// SpanAttr sets a key-value attribute on the current span
func (t *TraceStruct) SpanAttr(key string, value any) {
	if t.TraceSpan == nil {
		return
	}

	t.TraceSpan.SetAttributes(attribute.String(key, fmt.Sprintf("%v", value)))
}

// SpanAttrs sets attributes from a struct or map on the current span
func (t *TraceStruct) SpanAttrs(data any) {
	if t.TraceSpan == nil {
		return
	}

	iter := reflect.ValueOf(data)

	// Handle map
	if iter.Kind() == reflect.Map {
		for _, key := range iter.MapKeys() {
			value := iter.MapIndex(key)
			t.TraceSpan.SetAttributes(attribute.String(fmt.Sprintf("%v", key.Interface()), fmt.Sprintf("%v", value.Interface())))
		}
		return
	}

	if iter.Kind() != reflect.Struct {
		return
	}

	// Handle struct
	for i := 0; i < iter.NumField(); i++ {
		key := iter.Type().Field(i).Name
		value := iter.Field(i).Interface()
		t.TraceSpan.SetAttributes(attribute.String(key, fmt.Sprintf("%v", value)))
	}
}

// SpanError records an error on the current span
func (t *TraceStruct) SpanError(err error) *TraceStruct {
	if t.TraceSpan == nil || err == nil {
		return t
	}
	t.TraceSpan.RecordError(err)
	t.TraceSpan.SetStatus(codes.Error, err.Error())
	return t
}

// SpanSuccess marks the current span as successful
func (t *TraceStruct) SpanSuccess() *TraceStruct {
	if t.TraceSpan == nil {
		return t
	}

	t.TraceSpan.SetStatus(codes.Ok, "success")
	return t
}

// EndSpan ends the current span
func (t *TraceStruct) EndSpan() {
	if t.TraceSpan == nil {
		return
	}
	t.TraceSpan.End()
	t.TraceSpan = nil
	t.TraceSpanCtx = nil
}

// ChildSpan creates a child span under the current span
func (t *TraceStruct) ChildSpan(name string) {
	if t.TraceSpanCtx == nil {
		return
	}

	t.TraceChildSpanCtx, t.TraceChildSpan = eto.Trace().
		Name(name).
		FromContext(t.TraceSpanCtx).
		Start()
}

// ChildAttr sets a key-value attribute on the child span
func (t *TraceStruct) ChildAttr(key string, value any) {
	if t.TraceChildSpan == nil {
		return
	}

	t.TraceChildSpan.SetAttributes(attribute.String(key, fmt.Sprintf("%v", value)))
}

// ChildAttrs sets attributes from a struct or map on the child span
func (t *TraceStruct) ChildAttrs(data any) {
	if t.TraceChildSpan == nil {
		return
	}

	iter := reflect.ValueOf(data)

	// Handle map
	if iter.Kind() == reflect.Map {
		for _, key := range iter.MapKeys() {
			value := iter.MapIndex(key)
			t.TraceChildSpan.SetAttributes(attribute.String(fmt.Sprintf("%v", key.Interface()), fmt.Sprintf("%v", value.Interface())))
		}
		return
	}

	if iter.Kind() != reflect.Struct {
		return
	}

	// Handle struct
	for i := 0; i < iter.NumField(); i++ {
		key := iter.Type().Field(i).Name
		value := iter.Field(i).Interface()
		t.TraceChildSpan.SetAttributes(attribute.String(key, fmt.Sprintf("%v", value)))
	}
}

// ChildSpanError records an error on the child span
func (t *TraceStruct) ChildSpanError(err error) *TraceStruct {
	if t.TraceChildSpan == nil || err == nil {
		return t
	}

	t.TraceChildSpan.RecordError(err)
	t.TraceChildSpan.SetStatus(codes.Error, err.Error())
	return t
}

// ChildSpanSuccess marks the child span as successful
func (t *TraceStruct) ChildSpanSuccess() *TraceStruct {
	if t.TraceChildSpan == nil {
		return t
	}

	t.TraceChildSpan.SetStatus(codes.Ok, "success")
	return t
}

// EndChild ends the child span
func (t *TraceStruct) EndChild() {
	if t.TraceChildSpan == nil {
		return
	}
	t.TraceChildSpan.End()
	t.TraceChildSpan = nil
	t.TraceChildSpanCtx = nil
}
