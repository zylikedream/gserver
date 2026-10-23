package gxyredis

import (
	"context"
	"testing"

	"gserver/core/gxytrace"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TestProcessHookSpanIsChildOfMessageCtx 在已追踪的链路里,一条命令必须产生一个
// 子 span(父 = ctx 里的 span)。
func TestProcessHookSpanIsChildOfMessageCtx(t *testing.T) {
	exp := withRecordingProvider(t)
	parent := trace.SpanID{0xAB, 0xCD}
	ctx := tracedCtx(parent)

	cmd := redis.NewStringCmd(context.Background(), "SMOKE_SPAN")
	next := func(context.Context, redis.Cmder) error { return nil }
	if err := (metricsHook{}).ProcessHook(next)(ctx, cmd); err != nil {
		t.Fatalf("hook: %v", err)
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("span 数 = %d, want 1", len(spans))
	}
	if want := "redis." + cmd.FullName(); spans[0].Name != want {
		t.Fatalf("span 名 = %q, want %q", spans[0].Name, want)
	}
	if spans[0].Parent.SpanID() != parent {
		t.Fatalf("父 span = %s, want %s", spans[0].Parent.SpanID(), parent)
	}
}

// TestProcessHookNoSpanOutsideTrace 心跳续约、启动探活这类没有父链路的调用
// 不新开 trace。
func TestProcessHookNoSpanOutsideTrace(t *testing.T) {
	exp := withRecordingProvider(t)
	cmd := redis.NewStringCmd(context.Background(), "SMOKE_NO_PARENT")
	next := func(context.Context, redis.Cmder) error { return nil }
	_ = (metricsHook{}).ProcessHook(next)(context.Background(), cmd)
	if n := len(exp.GetSpans()); n != 0 {
		t.Fatalf("没有父链路时导出了 %d 个 span, want 0", n)
	}
}

// TestProcessHookSuppressesClientSpans 批处理标记生效时,逐条命令不再开 span
// (整段只应有一个阶段 span,数量放在属性里)。
func TestProcessHookSuppressesClientSpans(t *testing.T) {
	exp := withRecordingProvider(t)
	ctx := gxytrace.WithoutClientSpans(tracedCtx(trace.SpanID{1}))

	cmd := redis.NewStringCmd(context.Background(), "SMOKE_SUPPRESSED")
	next := func(context.Context, redis.Cmder) error { return nil }
	_ = (metricsHook{}).ProcessHook(next)(ctx, cmd)
	if n := len(exp.GetSpans()); n != 0 {
		t.Fatalf("抑制标记生效时导出了 %d 个 span, want 0", n)
	}
}

func tracedCtx(spanID trace.SpanID) context.Context {
	return trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{9},
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	}))
}

func withRecordingProvider(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return exp
}
