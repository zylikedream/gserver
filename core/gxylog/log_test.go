package gxylog

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// TestToZapFieldsInjectsTraceAndSpan 日志必须同时带 trace_id 与 span_id:
// 前者定位整条链路,后者定位本次处理;有 span 时不再注入 ctx_id。
func TestToZapFieldsInjectsTraceAndSpan(t *testing.T) {
	traceID := trace.TraceID{1, 2, 3}
	spanID := trace.SpanID{4, 5}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	}))

	got := fieldsToMap(toZapFields(ctx, nil))
	if got["trace_id"] != traceID.String() {
		t.Fatalf("trace_id = %q, want %q", got["trace_id"], traceID.String())
	}
	if got["span_id"] != spanID.String() {
		t.Fatalf("span_id = %q, want %q", got["span_id"], spanID.String())
	}
	if _, ok := got["ctx_id"]; ok {
		t.Fatal("有有效 span 时不应再注入 ctx_id")
	}
}

// TestToZapFieldsWithoutSpanFallsBack 没有有效 span 时不注入 trace_id/span_id。
func TestToZapFieldsWithoutSpanFallsBack(t *testing.T) {
	got := fieldsToMap(toZapFields(context.Background(), nil))
	if _, ok := got["trace_id"]; ok {
		t.Fatalf("不应注入 trace_id: %v", got)
	}
	if _, ok := got["span_id"]; ok {
		t.Fatalf("不应注入 span_id: %v", got)
	}
}

func fieldsToMap(fields []zap.Field) map[string]string {
	m := make(map[string]string, len(fields))
	for _, f := range fields {
		m[f.Key] = f.String
	}
	return m
}
