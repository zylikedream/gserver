package gxytrace

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type suppressClientSpansKey struct{}

// WithoutClientSpans 让 ctx 覆盖的范围内不再产生 I/O 客户端 span。
//
// 用于"逐项循环"这类批处理:一条链路里 N 次同类调用只应有一个阶段 span
// (数量记在属性里),明细交给指标(Redis/DB 的耗时直方图 + exemplar)。
// 没有这个标记时,全局 hook 会为每一次调用各开一个 span。
func WithoutClientSpans(ctx context.Context) context.Context {
	return context.WithValue(ctx, suppressClientSpansKey{}, true)
}

func clientSpansSuppressed(ctx context.Context) bool {
	v, ok := ctx.Value(suppressClientSpansKey{}).(bool)
	return ok && v
}

// StartClientSpan 在 ctx 已有被采样 span 时开一个 Client 子 span,否则返回 noop。
//
// 三条规则都收在这里:
//   - 没有采样中的父链路就不开:心跳续约、启动探活、定时任务、MQ 消费如果各自
//     新开 trace,后端会被单 span 的 trace 灌满;
//   - 被 WithoutClientSpans 标记的范围内不开(批处理只留阶段 span);
//   - 每次向全局 provider 取 tracer:包级缓存会在 provider 更换后失效
//     (全局委托只在第一次 SetTracerProvider 时切换已创建的 tracer)。
//
// 返回的 span 永不为 nil:未追踪时是 noop,调用方可以直接 defer End()。
func StartClientSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	noop := trace.SpanFromContext(context.Background())
	if ctx == nil || clientSpansSuppressed(ctx) || !trace.SpanContextFromContext(ctx).IsSampled() {
		return ctx, noop
	}
	return otel.Tracer("gxytrace.client").Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
}
