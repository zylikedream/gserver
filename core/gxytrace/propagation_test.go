package gxytrace

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// TestTraceparentRoundTrip 点对点通知依赖的往返:生产者注入的 traceparent
// 必须还原成同一个 trace/span,并标记为远端父。
func TestTraceparentRoundTrip(t *testing.T) {
	traceID := trace.TraceID{1, 2, 3}
	spanID := trace.SpanID{4, 5}
	producer := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	}))

	tp := TraceparentFromContext(producer)
	if tp == "" {
		t.Fatal("有有效 span 时必须生成 traceparent")
	}
	back := trace.SpanContextFromContext(ContextWithTraceparent(context.Background(), tp))
	if back.TraceID() != traceID || back.SpanID() != spanID {
		t.Fatalf("还原后 trace/span = %s/%s, want %s/%s", back.TraceID(), back.SpanID(), traceID, spanID)
	}
	if !back.IsRemote() {
		t.Fatal("从 payload 还原的 span 必须标记为远端")
	}

	if got := TraceparentFromContext(context.Background()); got != "" {
		t.Fatalf("无 span 时 traceparent = %q, want 空", got)
	}
}

// TestLinkedConsumerSpan 广播消费端:独立 trace + 指回生产者 span 的 link。
func TestLinkedConsumerSpan(t *testing.T) {
	exp := withRecordingProvider(t)

	producerTraceID := trace.TraceID{9, 9}
	producerSpanID := trace.SpanID{7, 7}
	producer := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    producerTraceID,
		SpanID:     producerSpanID,
		TraceFlags: trace.FlagsSampled,
	}))
	tp := TraceparentFromContext(producer)

	_, span := StartLinkedConsumerSpan(context.Background(), "mq.consume", tp)
	span.End()

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("span 数 = %d, want 1", len(spans))
	}
	if spans[0].SpanContext.TraceID() == producerTraceID {
		t.Fatal("广播消费者必须是独立 trace,不能挂在生产者的 trace 下")
	}
	if len(spans[0].Links) != 1 || spans[0].Links[0].SpanContext.SpanID() != producerSpanID {
		t.Fatalf("links = %v, want 一个指向 %s 的 link", spans[0].Links, producerSpanID)
	}
}
