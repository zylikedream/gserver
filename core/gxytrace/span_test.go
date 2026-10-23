package gxytrace

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// TestStartClientSpanRules 固定三条规则:
// 只在被采样的链路里开;抑制标记生效;未追踪时是 noop —— 绝不新开 trace。
func TestStartClientSpanRules(t *testing.T) {
	exp := withRecordingProvider(t)

	// 无父链路:noop,不产生 trace
	_, span := StartClientSpan(context.Background(), "probe.root")
	if span.IsRecording() {
		t.Fatal("无父链路时不应开新 trace")
	}
	span.End()
	if n := len(exp.GetSpans()); n != 0 {
		t.Fatalf("无父链路时导出了 %d 个 span, want 0", n)
	}

	// 有父链路:子 span,父 id 正确
	parent := trace.SpanID{0xAA, 0xBB}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     parent,
		TraceFlags: trace.FlagsSampled,
	}))
	_, child := StartClientSpan(ctx, "probe.child")
	if !child.IsRecording() {
		t.Fatal("被采样的链路里必须记录子 span")
	}
	child.End()
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("导出 span 数 = %d, want 1", len(spans))
	}
	if spans[0].Parent.SpanID() != parent {
		t.Fatalf("父 span = %s, want %s", spans[0].Parent.SpanID(), parent)
	}

	// 抑制标记:即使有父链路也不开
	_, suppressed := StartClientSpan(WithoutClientSpans(ctx), "probe.suppressed")
	if suppressed.IsRecording() {
		t.Fatal("抑制标记生效时不应开 span")
	}
	suppressed.End()
	if n := len(exp.GetSpans()); n != 1 {
		t.Fatalf("抑制后导出了 %d 个 span, want 1", n)
	}
}
