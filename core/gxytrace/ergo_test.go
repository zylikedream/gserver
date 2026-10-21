package gxytrace

import (
	"context"
	"testing"
	"time"

	"ergo.services/ergo"
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
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
		Point:        gen.TracingPointSent,
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

// TestContextWithErgoTrace 覆盖"运行时标识进 ctx"这件事的判据:
// 日志只在该 span **有效且被采样**时才注入 trace_id,因此这两条必须成立;
// 链路起点没有 span id、以及零值(不在被追踪链路上)是两个会静默退化的边界。
func TestContextWithErgoTrace(t *testing.T) {
	ids := [2]uint64{0x1122334455667788, 0x99aabbccddeeff00}
	want := traceIDFromErgo(ids)

	ctx := ContextWithErgoTrace(context.Background(), gen.Tracing{ID: ids, SpanID: 0x1234})
	sc := trace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() || !sc.IsSampled() {
		t.Fatalf("span 必须有效且被采样,否则日志拿不到 trace_id: %v", sc)
	}
	if sc.TraceID() != want {
		t.Fatalf("trace id = %s, want %s", sc.TraceID(), want)
	}
	// span_id 必须是"本次处理"那一段(与导出器 processed 的派生规则一致),
	// 不能是调用方 hop 的标识 —— 否则日志会指向调用方的 span。
	if sc.SpanID() != derivedSpanID(0x1234, gen.TracingPointProcessed) {
		t.Fatalf("span id = %s, want %s(processed)", sc.SpanID(),
			derivedSpanID(0x1234, gen.TracingPointProcessed))
	}
	if sc.SpanID() == spanIDFromErgo(0x1234) {
		t.Fatal("span id 不能等于调用方 hop 的标识")
	}

	// 链路起点没有 span id:仍须给出合法标识,否则 IsValid 为假、日志静默退化。
	root := trace.SpanFromContext(
		ContextWithErgoTrace(context.Background(), gen.Tracing{ID: ids})).SpanContext()
	if !root.IsValid() || root.TraceID() != want {
		t.Fatalf("没有 span id 的链路起点也必须可用: %v", root)
	}

	// 零值表示本条消息不在被追踪的链路上:原样返回,日志退化成 ctx_id。
	if trace.SpanFromContext(
		ContextWithErgoTrace(context.Background(), gen.Tracing{})).SpanContext().IsValid() {
		t.Fatal("零值不应产生 span context")
	}
}

// TestErgoExporterSeparatesHopObservationPoints 同一 hop 的 sent/delivered/processed
// 必须映射成三个不同的 span id,且后两者挂在该 hop 的 sent 下。
// 为什么需要:运行时给这三个观测点**同一个 SpanID**(见下一个测试的真实节点验证),
// 原样映射会让三个 span 在后端按 (trace_id, span_id) 互相覆盖 ——
// processed 上的业务属性与真实处理耗时都会丢。
func TestErgoExporterSeparatesHopObservationPoints(t *testing.T) {
	exp := withRecordingProvider(t)
	traceID := [2]uint64{0xAAAA000000000000, 0xBBBB000000000000}
	const hop uint64 = 0x1234
	const parent uint64 = 0x9999

	e := NewErgoExporter()
	for _, p := range []gen.TracingPoint{
		gen.TracingPointSent, gen.TracingPointDelivered, gen.TracingPointProcessed,
	} {
		e.HandleSpan(gen.TracingSpan{
			TraceID: traceID, SpanID: hop, ParentSpanID: parent, Point: p,
			Timestamp: time.Now().UnixNano(),
		})
	}

	spans := exp.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("导出 span 数 = %d, want 3", len(spans))
	}
	sentID := spanIDFromErgo(hop)
	seen := make(map[string]bool, len(spans))
	for i, sp := range spans {
		sid := sp.SpanContext.SpanID().String()
		if seen[sid] {
			t.Fatalf("span id %s 重复:同 hop 的不同观测点必须可区分", sid)
		}
		seen[sid] = true

		if i == 0 { // sent 保留运行时 id,父为该 hop 的 ParentSpanID
			if sp.SpanContext.SpanID() != sentID {
				t.Fatalf("sent 的 span id = %s, want %s", sp.SpanContext.SpanID(), sentID)
			}
			if sp.Parent.SpanID() != spanIDFromErgo(parent) {
				t.Fatalf("sent 的父 = %s, want %s", sp.Parent.SpanID(), spanIDFromErgo(parent))
			}
			continue
		}
		if sp.Parent.SpanID() != sentID {
			t.Fatalf("第 %d 个观测的父 = %s, want 本 hop 的 sent(%s)", i, sp.Parent.SpanID(), sentID)
		}
	}
}

// TestErgoExporterRealHopSpanIDs 用真实节点验证上面测试的前提:运行时的三个观测点
// 确实带同一个 SpanID。若将来运行时改为每点独立 id,本测试会红,提示重审映射。
type probeActor struct{ act.Actor }

func (a *probeActor) Init(_ ...any) error { return nil }

func TestErgoExporterRealHopSpanIDs(t *testing.T) {
	exp := withRecordingProvider(t)

	node, err := ergo.StartNode("spanid_probe@localhost", gen.NodeOptions{})
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	t.Cleanup(func() { node.Stop() })
	if err := node.TracingExporterAdd("probe", NewErgoExporter(),
		gen.TracingFlagSend|gen.TracingFlagReceive); err != nil {
		t.Fatalf("add exporter: %v", err)
	}
	if err := node.SetTracingSampler(gen.TracingSamplerRatio(1)); err != nil {
		t.Fatalf("sampler: %v", err)
	}

	pid, err := node.Spawn(func() gen.ProcessBehavior { return &probeActor{} }, gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if err := node.Send(pid, "hello"); err != nil {
		t.Fatalf("send: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for len(exp.GetSpans()) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	spans := exp.GetSpans()
	if len(spans) < 3 {
		t.Fatalf("导出 span 数 = %d, want 3(sent/delivered/processed)", len(spans))
	}

	// 运行时三个观测共用一个 hop id -> 我们的映射必须让导出后的 id 互不相同。
	ids := make(map[string]bool, len(spans))
	for _, sp := range spans {
		if ids[sp.SpanContext.SpanID().String()] {
			t.Fatalf("导出后仍有重复 span id: %v", sp.SpanContext.SpanID())
		}
		ids[sp.SpanContext.SpanID().String()] = true
	}
}
