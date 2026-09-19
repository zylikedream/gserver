package gxytrace

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"time"

	"ergo.services/ergo/gen"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// 本文件把运行时的追踪观测转成 OTel span 送入既有导出器(ADR 0013)。
//
// 关键约束是**标识必须原样保留**:运行时已经在自己的追踪上下文里维护了
// 整条链路的父子关系与追踪标识,若这里另生成一套,跨节点的链路就会断成
// 互不相干的若干条 trace。做法是让 ID 生成器从 context 取运行时给的标识
// (见 ergoIDs),而不是自己随机生成。

// ergoIDs 是运行时在本次观测中使用的追踪标识。
// 经 context 传递给 ID 生成器,使生成的 span 使用同一套标识。
type ergoIDs struct {
	traceID trace.TraceID
	spanID  trace.SpanID
}

type ergoIDsKey struct{}

// traceIDFromErgo 把运行时的 128 位追踪标识转成 OTel 的 16 字节形式。
//
// 两个方向都不存在"语义转换",只是同一串比特的搬运;父子的标识都经由本函数,
// 因此字节序只需自洽,与运行时内部的解释方式无关。
func traceIDFromErgo(id [2]uint64) trace.TraceID {
	var out trace.TraceID
	binary.BigEndian.PutUint64(out[0:8], id[0])
	binary.BigEndian.PutUint64(out[8:16], id[1])
	return out
}

// spanIDFromErgo 把运行时的 64 位观测标识转成 OTel 的 8 字节形式。
func spanIDFromErgo(id uint64) trace.SpanID {
	var out trace.SpanID
	binary.BigEndian.PutUint64(out[:], id)
	return out
}

// ergoIDGenerator 让 span 直接采用运行时给出的标识。
// 无运行时标识时(业务自行创建的 span)退回随机生成。
type ergoIDGenerator struct{}

// NewIDs 返回新 trace 的标识。仅当 context 未携带运行时标识时随机生成。
func (ergoIDGenerator) NewIDs(ctx context.Context) (trace.TraceID, trace.SpanID) {
	if ids, ok := ctx.Value(ergoIDsKey{}).(ergoIDs); ok {
		return ids.traceID, ids.spanID
	}
	return randomTraceID(), randomSpanID()
}

// NewSpanID 返回已有 trace 内的新观测标识,同样优先采用运行时给出的值。
func (ergoIDGenerator) NewSpanID(ctx context.Context, _ trace.TraceID) trace.SpanID {
	if ids, ok := ctx.Value(ergoIDsKey{}).(ergoIDs); ok {
		return ids.spanID
	}
	return randomSpanID()
}

func randomTraceID() trace.TraceID {
	var tid trace.TraceID
	for {
		binary.NativeEndian.PutUint64(tid[0:8], rand.Uint64())
		binary.NativeEndian.PutUint64(tid[8:16], rand.Uint64())
		if tid.IsValid() {
			return tid
		}
	}
}

func randomSpanID() trace.SpanID {
	var sid trace.SpanID
	for {
		binary.NativeEndian.PutUint64(sid[:], rand.Uint64())
		if sid.IsValid() {
			return sid
		}
	}
}

// ErgoExporter 把运行时的追踪观测转成 OTel span。
//
// 运行时的每次观测(sent/delivered/processed/业务区间)各对应一个 span:
// 它给的是观测点而非区间,因此除业务区间外 span 时长为零——这是运行时的
// 观测粒度,不是丢失了耗时。
type ErgoExporter struct {
	tracer trace.Tracer
}

// NewErgoExporter 创建导出器。取全局 tracer,因此导出目标由既有追踪配置决定。
func NewErgoExporter() *ErgoExporter {
	return &ErgoExporter{tracer: otel.Tracer("ergo.runtime")}
}

// HandleSpan 处理一次观测。由运行时在专用 worker 上串行调用。
func (e *ErgoExporter) HandleSpan(s gen.TracingSpan) {
	start := time.Unix(0, s.Timestamp)
	end := start
	if s.EndTimestamp > 0 {
		end = time.Unix(0, s.EndTimestamp)
	}

	ctx := context.Background()
	// 父观测的标识即 OTel 意义上的父 span——两边标识同源(见上),直接对应。
	if s.ParentSpanID != 0 {
		ctx = trace.ContextWithRemoteSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceIDFromErgo(s.TraceID),
			SpanID:     spanIDFromErgo(s.ParentSpanID),
			TraceFlags: trace.FlagsSampled,
			Remote:     true,
		}))
	}
	ctx = context.WithValue(ctx, ergoIDsKey{}, ergoIDs{
		traceID: traceIDFromErgo(s.TraceID),
		spanID:  spanIDFromErgo(s.SpanID),
	})

	// 派生出的 ctx 不再往下传:本函数只处理这一条观测,span 在末尾显式结束。
	_, span := e.tracer.Start(ctx, spanName(s),
		trace.WithTimestamp(start),
		trace.WithSpanKind(spanKind(s)),
		trace.WithAttributes(
			attribute.String("ergo.node", string(s.Node)),
			attribute.String("ergo.point", s.Point.String()),
			attribute.String("ergo.from", s.From.String()),
			attribute.String("ergo.to", fmt.Sprint(s.To)),
		),
	)
	if s.Kind != 0 {
		span.SetAttributes(attribute.String("ergo.kind", s.Kind.String()))
	}
	if s.Behavior != "" {
		span.SetAttributes(attribute.String("ergo.behavior", s.Behavior))
	}
	if s.Ref != (gen.Ref{}) {
		span.SetAttributes(attribute.String("ergo.ref", s.Ref.String()))
	}
	for _, a := range s.Attributes {
		span.SetAttributes(attribute.String(a.Key, a.Value))
	}
	if s.Error != "" {
		span.SetStatus(codes.Error, s.Error)
		span.SetAttributes(attribute.String("ergo.error", s.Error))
	}
	span.End(trace.WithTimestamp(end))
}

// Terminate 在导出器被移除或节点停止时调用。
//
// 此处不做刷新:span 由既有 TracerProvider 的批处理器持有,其 Shutdown 才是
// 刷新的位置;追踪模块先于 actor 模块启动、后于其停止,时序上够用。
func (e *ErgoExporter) Terminate() {}

// spanName 生成可读的 span 名:业务区间用行为名,其余用"观测点 + 消息类型"。
func spanName(s gen.TracingSpan) string {
	if s.Point == gen.TracingPointSpan {
		if s.Behavior != "" {
			return s.Behavior
		}
		return "span"
	}
	if s.Message != "" {
		return s.Point.String() + " " + s.Message
	}
	if s.Kind != 0 {
		return s.Point.String() + " " + s.Kind.String()
	}
	return s.Point.String()
}

// spanKind 把运行时的操作类型映射到 OTel 的 span 类型。
//
// 映射依据是"谁在等待":异步投递是本方主动产生(producer),被请求方处理
// 请求时才有对端在等(consumer),其余是进程内部动作。
func spanKind(s gen.TracingSpan) trace.SpanKind {
	switch s.Kind {
	case gen.TracingKindSend:
		if s.Point == gen.TracingPointProcessed {
			return trace.SpanKindConsumer
		}
		return trace.SpanKindProducer
	case gen.TracingKindRequest:
		if s.Point == gen.TracingPointProcessed {
			return trace.SpanKindServer
		}
		return trace.SpanKindClient
	case gen.TracingKindResponse:
		return trace.SpanKindClient
	default:
		return trace.SpanKindInternal
	}
}
