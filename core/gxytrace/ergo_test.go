package gxytrace

import (
	"testing"
	"time"

	"ergo.services/ergo/gen"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// withRecordingProvider 把全局 provider 换成同步记录到内存的实例,
// 并采用与生产相同的 ID 生成器——否则测的就不是生产路径。
func withRecordingProvider(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithIDGenerator(ergoIDGenerator{}),
	)
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(t.Context())
	})
	return exp
}

// 运行时给出的追踪标识必须原样出现在导出的 span 上。
// 若这里另生成一套标识,跨节点的链路会断成互不相干的若干条 trace,
// 排障时看到的就是"每次投递都是一条独立链路"。
func TestErgoExporterPreservesTraceIdentity(t *testing.T) {
	exp := withRecordingProvider(t)

	traceID := [2]uint64{0x1111111111111111, 0x2222222222222222}
	const spanID uint64 = 0x3333333333333333
	const parentSpanID uint64 = 0x4444444444444444

	NewErgoExporter().HandleSpan(gen.TracingSpan{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Point:        gen.TracingPointProcessed,
		Kind:         gen.TracingKindRequest,
		Timestamp:    time.Now().UnixNano(),
		Node:         "game@127.0.0.1",
	})

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("导出的 span 数 = %d, want 1", len(spans))
	}
	got := spans[0]
	if got.SpanContext.TraceID() != traceIDFromErgo(traceID) {
		t.Fatalf("trace id = %s, want %s", got.SpanContext.TraceID(), traceIDFromErgo(traceID))
	}
	if got.SpanContext.SpanID() != spanIDFromErgo(spanID) {
		t.Fatalf("span id = %s, want %s", got.SpanContext.SpanID(), spanIDFromErgo(spanID))
	}
	if got.Parent.SpanID() != spanIDFromErgo(parentSpanID) {
		t.Fatalf("parent span id = %s, want %s", got.Parent.SpanID(), spanIDFromErgo(parentSpanID))
	}
	// 父子必须在同一条 trace 内,否则链路仍然是断的。
	if got.Parent.TraceID() != got.SpanContext.TraceID() {
		t.Fatalf("parent trace id = %s, 与自身 trace id 不同", got.Parent.TraceID())
	}
}

// 根观测没有父:不得凭空造出一个父 span。
func TestErgoExporterRootSpanHasNoParent(t *testing.T) {
	exp := withRecordingProvider(t)

	NewErgoExporter().HandleSpan(gen.TracingSpan{
		TraceID:   [2]uint64{1, 2},
		SpanID:    3,
		Point:     gen.TracingPointSent,
		Kind:      gen.TracingKindSend,
		Timestamp: time.Now().UnixNano(),
	})

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("导出的 span 数 = %d, want 1", len(spans))
	}
	if spans[0].Parent.IsValid() {
		t.Fatalf("根观测不应有父 span,实际 parent=%s", spans[0].Parent.SpanID())
	}
}

// 业务区间带结束时刻,其时长必须被保留;观测点没有结束时刻,时长应为零。
func TestErgoExporterSpanDuration(t *testing.T) {
	exp := withRecordingProvider(t)
	start := time.Now()

	exp2 := NewErgoExporter()
	exp2.HandleSpan(gen.TracingSpan{
		TraceID: [2]uint64{1, 2}, SpanID: 3,
		Point: gen.TracingPointSpan, Behavior: "handler",
		Timestamp: start.UnixNano(), EndTimestamp: start.Add(50 * time.Millisecond).UnixNano(),
	})
	exp2.HandleSpan(gen.TracingSpan{
		TraceID: [2]uint64{1, 2}, SpanID: 4,
		Point: gen.TracingPointSent, Kind: gen.TracingKindSend,
		Timestamp: start.UnixNano(),
	})

	spans := exp.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("导出的 span 数 = %d, want 2", len(spans))
	}
	if got := spans[0].EndTime.Sub(spans[0].StartTime); got != 50*time.Millisecond {
		t.Fatalf("业务区间时长 = %v, want 50ms", got)
	}
	if got := spans[1].EndTime.Sub(spans[1].StartTime); got != 0 {
		t.Fatalf("观测点时长 = %v, want 0", got)
	}
}

// 错误必须反映在 span 状态上,否则链路里看不到失败。
func TestErgoExporterMarksError(t *testing.T) {
	exp := withRecordingProvider(t)

	NewErgoExporter().HandleSpan(gen.TracingSpan{
		TraceID: [2]uint64{1, 2}, SpanID: 3,
		Point: gen.TracingPointProcessed, Kind: gen.TracingKindRequest,
		Timestamp: time.Now().UnixNano(),
		Error:     "boom",
	})

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("导出的 span 数 = %d, want 1", len(spans))
	}
	if spans[0].Status.Code.String() != "Error" {
		t.Fatalf("状态 = %v, want Error", spans[0].Status.Code)
	}
}
