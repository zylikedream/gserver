package gxytrace

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TraceparentKey 是 W3C traceparent 在消息 payload 里使用的字段名。
const TraceparentKey = "traceparent"

// traceparentPropagator 直接用 TraceContext 而不读全局配置:payload 里只带
// traceparent,不受进程内 propagator 配置(是否合并 baggage)影响。
var traceparentPropagator = propagation.TraceContext{}

// TraceparentFromContext 生成 traceparent 字符串;当前 ctx 没有有效 span 时返回 ""。
func TraceparentFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	carrier := propagation.MapCarrier{}
	traceparentPropagator.Inject(ctx, carrier)
	return carrier.Get(TraceparentKey)
}

// ContextWithTraceparent 把消息里带来的链路上下文装回 ctx(远端父)。
//
// 用于**点对点**通知(1:1):消费者由此继续生产者的 trace,日志与子 span 都归属
// 那条链路。注意跨到 actor 的那一跳不继承它——运行时对"节点发起的消息"按节点级
// 采样另行决定,链路在那里另起。
func ContextWithTraceparent(ctx context.Context, traceparent string) context.Context {
	if traceparent == "" {
		return ctx
	}
	return traceparentPropagator.Extract(ctx, propagation.MapCarrier{TraceparentKey: traceparent})
}

// StartLinkedConsumerSpan 为一条**广播**消息开独立的 Consumer span,并用生产者
// span 作为 link。
//
// 一对多不继续生产者的 trace:否则单条 trace 的分支数随节点数膨胀。独立的 span
// 意味着它按**本地采样率**被采样——生产者被采样并不保证消费者被记录。
func StartLinkedConsumerSpan(ctx context.Context, name, traceparent string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attrs...),
	}
	if sc, ok := spanContextFromTraceparent(traceparent); ok {
		opts = append(opts, trace.WithLinks(trace.Link{SpanContext: sc}))
	}
	// 清掉可能存在的父 span:广播消费者始终是一条独立 trace。
	return otel.Tracer("gxytrace.mq").Start(
		trace.ContextWithSpanContext(ctx, trace.SpanContext{}), name, opts...)
}

func spanContextFromTraceparent(traceparent string) (trace.SpanContext, bool) {
	if traceparent == "" {
		return trace.SpanContext{}, false
	}
	ctx := traceparentPropagator.Extract(context.Background(),
		propagation.MapCarrier{TraceparentKey: traceparent})
	sc := trace.SpanContextFromContext(ctx)
	return sc, sc.IsValid()
}
