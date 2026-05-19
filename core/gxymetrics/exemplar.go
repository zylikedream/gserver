package gxymetrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
)

func ObserveWithTrace(ctx context.Context, observer prometheus.Observer, value float64) {
	traceID := TraceIDFromContext(ctx)
	if traceID == "" {
		observer.Observe(value)
		return
	}
	if exemplarObserver, ok := observer.(prometheus.ExemplarObserver); ok {
		exemplarObserver.ObserveWithExemplar(value, prometheus.Labels{"trace_id": traceID})
		return
	}
	observer.Observe(value)
}

func TraceIDFromContext(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() || !spanCtx.TraceID().IsValid() {
		return ""
	}
	return spanCtx.TraceID().String()
}
