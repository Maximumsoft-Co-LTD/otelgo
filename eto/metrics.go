package eto

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	counterMu      sync.Mutex
	counterCache   = map[string]metric.Int64Counter{}
	histogramMu    sync.Mutex
	histogramCache = map[string]metric.Float64Histogram{}
)

type CounterBuilder struct {
	name  string
	attrs []attribute.KeyValue
	unit  string
	desc  string
}

func MetricCounter(name string) *CounterBuilder {
	return &CounterBuilder{
		name: name,
		unit: "1",
	}
}

func (b *CounterBuilder) Attr(key string, val any) *CounterBuilder {
	b.attrs = append(b.attrs, anyToAttr(key, val))
	return b
}

func (b *CounterBuilder) Attrs(attrs ...attribute.KeyValue) *CounterBuilder {
	b.attrs = append(b.attrs, attrs...)
	return b
}

func (b *CounterBuilder) Unit(unit string) *CounterBuilder {
	if unit != "" {
		b.unit = unit
	}
	return b
}

func (b *CounterBuilder) Description(desc string) *CounterBuilder {
	b.desc = desc
	return b
}

func (b *CounterBuilder) Add(ctx context.Context, value int64) {
	if !globalCfg.EnableMetrics || globalMeter == nil {
		return
	}

	counter := getOrCreateCounter(b.name, b.unit, b.desc)
	if counter == nil {
		// สร้าง instrument ไม่ได้ → ไม่ต้องทำอะไร
		return
	}

	counter.Add(ctx, value, metric.WithAttributes(b.attrs...))
}

func getOrCreateCounter(name, unit, desc string) metric.Int64Counter {
	counterMu.Lock()
	defer counterMu.Unlock()

	if c, ok := counterCache[name]; ok {
		return c
	}

	c, err := globalMeter.Int64Counter(
		name,
		metric.WithUnit(unit),
		metric.WithDescription(desc),
	)
	if err != nil {
		// อย่า panic / log ซ้ำไปซ้ำมา แค่ไม่ส่ง metric พอ
		return nil
	}
	counterCache[name] = c
	return c
}

type HistogramBuilder struct {
	name  string
	attrs []attribute.KeyValue
	unit  string
	desc  string
}

func MetricHistogram(name string) *HistogramBuilder {
	return &HistogramBuilder{
		name: name,
		unit: "ms",
	}
}

func (b *HistogramBuilder) Attr(key string, val any) *HistogramBuilder {
	b.attrs = append(b.attrs, anyToAttr(key, val))
	return b
}

func (b *HistogramBuilder) Attrs(attrs ...attribute.KeyValue) *HistogramBuilder {
	b.attrs = append(b.attrs, attrs...)
	return b
}

func (b *HistogramBuilder) Unit(unit string) *HistogramBuilder {
	if unit != "" {
		b.unit = unit
	}
	return b
}

func (b *HistogramBuilder) Description(desc string) *HistogramBuilder {
	b.desc = desc
	return b
}

func (b *HistogramBuilder) Record(ctx context.Context, value float64) {
	if !globalCfg.EnableMetrics || globalMeter == nil {
		return
	}

	h := getOrCreateHistogram(b.name, b.unit, b.desc)
	if h == nil {
		return
	}

	h.Record(ctx, value, metric.WithAttributes(b.attrs...))
}

func getOrCreateHistogram(name, unit, desc string) metric.Float64Histogram {
	histogramMu.Lock()
	defer histogramMu.Unlock()

	if h, ok := histogramCache[name]; ok {
		return h
	}

	h, err := globalMeter.Float64Histogram(
		name,
		metric.WithUnit(unit),
		metric.WithDescription(desc),
	)
	if err != nil {
		return nil
	}
	histogramCache[name] = h
	return h
}

func anyToAttr(key string, val any) attribute.KeyValue {
	switch v := val.(type) {
	case string:
		return attribute.String(key, v)
	case int:
		return attribute.Int(key, v)
	case int64:
		return attribute.Int64(key, v)
	case float64:
		return attribute.Float64(key, v)
	case bool:
		return attribute.Bool(key, v)
	default:
		return attribute.String(key, fmt.Sprintf("%v", v))
	}
}
