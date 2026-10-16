package gxyactor

import (
	"context"
	"testing"
	"time"

	"gserver/protocol/pb"

	"ergo.services/ergo"
	"ergo.services/ergo/gen"
	"go.opentelemetry.io/otel/trace"
)

// 入口 ctx 的链路标识来自运行时的"当前传播链路":它在消息带链路时激活、
// 处理结束即恢复。因此只有**被采样**的消息会带上标识,未采样的必须退化成
// actor 级 ctx(日志退化为 ctx_id),不能凭空造一个 trace_id。

// traceCtxProbe 把业务入口拿到的链路标识交回测试。
type traceCtxProbe struct {
	*BaseActor
	got chan trace.SpanContext
}

func (p *traceCtxProbe) HandleMessage(ctx context.Context, msg any) (any, error) {
	if _, ok := msg.(actorInitMsg); ok { // 门面自投的初始化消息,不算外部消息
		return nil, nil
	}
	p.got <- trace.SpanFromContext(ctx).SpanContext()
	return nil, nil
}

// probeMessage 送一条消息进去,返回业务入口看到的链路标识。
func probeMessage(t *testing.T, sampler gen.TracingSampler) trace.SpanContext {
	t.Helper()

	node, err := ergo.StartNode("trace_probe@localhost", gen.NodeOptions{})
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	t.Cleanup(func() { node.Stop() })
	if sampler != nil {
		if err := node.SetTracingSampler(sampler); err != nil {
			t.Fatalf("set sampler: %v", err)
		}
	}

	got := make(chan trace.SpanContext, 1)
	pid, err := node.Spawn(ActorFactory("trace_probe", func() ActorBehavior {
		return &traceCtxProbe{BaseActor: NewBaseActor(), got: got}
	}), gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if err := node.Send(pid, &pb.Ack{}); err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case sc := <-got:
		return sc
	case <-time.After(5 * time.Second):
		t.Fatal("业务入口未被调用")
		return trace.SpanContext{}
	}
}

// TestBusinessEntryCtxCarriesMessageTrace 被采样的消息:入口 ctx 必须带该链路的
// 标识(日志据此注入 trace_id,见 gxylog)。
func TestBusinessEntryCtxCarriesMessageTrace(t *testing.T) {
	sc := probeMessage(t, gen.TracingSamplerRatio(1))
	if !sc.IsValid() || !sc.IsSampled() {
		t.Fatalf("被采样的消息,入口 ctx 必须带链路标识: %v", sc)
	}
}

// TestBusinessEntryCtxWithoutTrace 未被采样的消息:入口 ctx 不带链路标识,
// 日志退化成 ctx_id——凭空造 trace_id 会把人指向一条不存在的链路。
func TestBusinessEntryCtxWithoutTrace(t *testing.T) {
	if sc := probeMessage(t, nil); sc.IsValid() {
		t.Fatalf("未采样的消息不应带链路标识: %v", sc)
	}
}
