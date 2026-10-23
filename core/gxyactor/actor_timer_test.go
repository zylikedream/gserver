package gxyactor

import (
	"context"
	"testing"
	"time"

	"gserver/core/gxytrace"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TestActorTimer_TimerCallbackTracing: 定时回调应拿到一条独立 trace,
// span 名与属性固定;采样关闭(SampleRate 为 0)时不产生 span,回调照常执行。
func TestActorTimer_TimerCallbackTracing(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	defer otel.SetTracerProvider(prev)

	timer := newActorTimer(nil)
	spanValid := false
	timer.callbackFuncs["probe"] = func(ctx context.Context) {
		spanValid = trace.SpanFromContext(ctx).SpanContext().IsValid()
	}

	// 未初始化 trace app 时采样率为 0:不产生 span。
	// (若同包其它用例已初始化过 trace app,则跳过这一段,避免依赖执行顺序。)
	if gxytrace.SampleRate() <= 0 {
		timer.Active(context.Background(), ActorTimerMsg{Name: "probe"})
		if spanValid {
			t.Fatal("span created while tracing disabled")
		}
		if n := len(recorder.Started()); n != 0 {
			t.Fatalf("recorded %d spans while tracing disabled", n)
		}
	}

	// 打开追踪:每次 tick 一条 span。
	gxytrace.NewTraceApp()
	timer.Active(context.Background(), ActorTimerMsg{Name: "probe"})
	if !spanValid {
		t.Fatal("callback ctx carries no span")
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	if got := spans[0].Name(); got != "timer.probe" {
		t.Fatalf("span name = %q, want timer.probe", got)
	}
}

// TestActorTimer_AddTickIsIdempotent: 同名 AddTick 只更新回调,不重复注册运行时定时器。
// 预填 timers 后调用 AddTick——守卫在触碰运行时之前返回,所以这里不需要真实 rt:
// 若守卫被删掉,会调用 nil 的 rt.SendEvery 直接 panic。
func TestActorTimer_AddTickIsIdempotent(t *testing.T) {
	timer := newActorTimer(nil)
	timer.timers["probe"] = func() bool { return true }

	fired := 0
	timer.AddTick("probe", time.Second, func(context.Context) { fired++ })

	if _, ok := timer.callbackFuncs["probe"]; !ok {
		t.Fatal("callback not registered")
	}
	timer.Active(context.Background(), ActorTimerMsg{Name: "probe"})
	if fired != 1 {
		t.Fatalf("callback fired %d times, want 1", fired)
	}
}

// TestActorTimer_CancelStopsRuntimeTimer: Cancel 必须调用运行时的取消函数,
// 而不是只删回调——否则运行时定时器永不回收,同名重新注册会叠加触发。
func TestActorTimer_CancelStopsRuntimeTimer(t *testing.T) {
	timer := newActorTimer(nil)
	cancelled := 0
	timer.timers["probe"] = func() bool { cancelled++; return true }
	timer.ones["probe"] = struct{}{}
	timer.callbackFuncs["probe"] = func(context.Context) {}

	timer.Cancel(context.Background(), "probe")

	if cancelled != 1 {
		t.Fatalf("runtime timer not cancelled: %d", cancelled)
	}
	if _, ok := timer.timers["probe"]; ok {
		t.Fatal("timer entry not removed")
	}
	if _, ok := timer.callbackFuncs["probe"]; ok {
		t.Fatal("callback not removed")
	}
}

// TestActorTimer_OnceClearsNameAfterFire: 一次性定时触发后必须释放名字,
// 否则下一次同名 AddOnce 会以为还有未触发的定时,永远不再注册。
func TestActorTimer_OnceClearsNameAfterFire(t *testing.T) {
	timer := newActorTimer(nil)
	timer.timers["probe"] = func() bool { return true }
	timer.ones["probe"] = struct{}{}
	fired := 0
	timer.callbackFuncs["probe"] = func(context.Context) { fired++ }

	timer.Active(context.Background(), ActorTimerMsg{Name: "probe"})

	if fired != 1 {
		t.Fatalf("callback fired %d times, want 1", fired)
	}
	if _, ok := timer.timers["probe"]; ok {
		t.Fatal("one-shot timer entry not cleared")
	}
	if _, ok := timer.ones["probe"]; ok {
		t.Fatal("one-shot mark not cleared")
	}
	if _, ok := timer.callbackFuncs["probe"]; ok {
		t.Fatal("one-shot callback not cleared")
	}
}
