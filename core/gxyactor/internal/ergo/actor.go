package ergo

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gserver/core/gxyactor"
	"gserver/core/gxyutil"
)

type ergoActor struct {
	act.Actor
	adapter  *Adapter
	kind     string
	callerID string
	id       string
	host     *gxyactor.ActorProcess
	ctx      *actorContext
}

func newErgoActor(adapter *Adapter, kind, callerID, id string, producer gxyactor.ActorProducer) *ergoActor {
	return &ergoActor{
		adapter:  adapter,
		kind:     kind,
		callerID: callerID,
		id:       id,
		host:     gxyactor.NewActorProcess(kind, producer()),
	}
}

func (a *ergoActor) Init(args ...any) error {
	if a.host == nil {
		return ErrActorInitFailed
	}
	a.adapter.remember(a.kind, a.callerID, a.PID())
	a.ctx = &actorContext{adapter: a.adapter, process: a}
	a.prepareContext()
	if err := a.host.Init(a.ctx, args); err != nil {
		return wrap(ErrActorInitFailed, err)
	}
	return nil
}

func (a *ergoActor) HandleMessage(from gen.PID, message any) error {
	decoded, err := a.adapter.decode(message)
	if err != nil {
		return err
	}
	switch down := decoded.(type) {
	case gen.MessageDownPID:
		decoded = gxyactor.ActorTerminatedMessage{Who: a.adapter.fromErgoPID(down.PID, "")}
	case *gen.MessageDownPID:
		if down != nil {
			decoded = gxyactor.ActorTerminatedMessage{Who: a.adapter.fromErgoPID(down.PID, "")}
		}
	}
	if a.ctx == nil {
		a.ctx = &actorContext{adapter: a.adapter, process: a}
	}
	a.ctx.sender = a.adapter.fromErgoPID(from, "")
	a.ctx.request = nil
	a.prepareContext()
	if _, unspanned := decoded.(gxyactor.IUnspanMessage); unspanned {
		return a.host.HandleMessage(a.ctx, decoded)
	}
	previous := a.ctx.ctx
	parent := trace.SpanContextFromContext(previous)
	if parent.IsRemote() {
		span := trace.SpanFromContext(previous)
		span.SetName(fmt.Sprintf("%T", decoded))
		span.SetAttributes(attribute.String("actor_kind", a.kind), attribute.String("msg", gxyutil.FormatObject(decoded)))
		return a.host.HandleMessage(a.ctx, decoded)
	}
	ctx, span := otel.Tracer("gserver/actor").Start(previous, fmt.Sprintf("%T", decoded))
	span.SetAttributes(attribute.String("actor_kind", a.kind), attribute.String("msg", gxyutil.FormatObject(decoded)))
	a.ctx.ctx = ctx
	defer func() {
		span.End()
		a.ctx.ctx = previous
	}()
	if err := a.host.HandleMessage(a.ctx, decoded); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

func (a *ergoActor) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	decoded, err := a.adapter.decode(request)
	if err != nil {
		return nil, err
	}
	if a.ctx == nil {
		a.ctx = &actorContext{adapter: a.adapter, process: a}
	}
	a.ctx.sender = a.adapter.fromErgoPID(from, "")
	a.ctx.request = &ergoRequest{sender: a.ctx.sender, rawSender: from, ref: ref, process: a}
	a.prepareContext()
	result, err := a.host.HandleCall(a.ctx, decoded)
	if err != nil {
		_ = a.SendResponseError(from, ref, err)
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	wire, err := a.adapter.encodeFor(a.ctx.sender, result)
	if err != nil {
		return nil, err
	}
	return wire, nil
}

func (a *ergoActor) Terminate(reason error) {
	if a.host != nil {
		if a.ctx == nil {
			a.ctx = &actorContext{adapter: a.adapter, process: a}
		}
		a.prepareContext()
		a.host.Terminate(a.ctx, reason)
	}
	if a.adapter != nil {
		a.adapter.forgetRaw(a.PID())
	}
}

func (a *ergoActor) prepareContext() {
	incoming := contextWithErgoTrace(context.Background(), a.PropagatingTrace(), a)
	a.ctx.ctx = a.host.Context(incoming, a.adapter)
}

type ergoRequest struct {
	sender    gxyactor.PID
	rawSender gen.PID
	ref       gen.Ref
	process   *ergoActor
}

func (r *ergoRequest) Sender() gxyactor.PID {
	if r == nil {
		return gxyactor.PID{}
	}
	return r.sender
}

type processContextKey struct{}

type actorContext struct {
	adapter *Adapter
	process *ergoActor
	sender  gxyactor.PID
	request *ergoRequest
	ctx     context.Context
}

func contextWithErgoTrace(base context.Context, tracing gen.Tracing, process gen.Process) context.Context {
	if base == nil {
		base = context.Background()
	}
	base = context.WithValue(base, processContextKey{}, process)
	if tracing.ID == [2]uint64{} || tracing.SpanID == 0 {
		return base
	}
	var traceID trace.TraceID
	binary.BigEndian.PutUint64(traceID[:8], tracing.ID[0])
	binary.BigEndian.PutUint64(traceID[8:], tracing.ID[1])
	var spanID trace.SpanID
	binary.BigEndian.PutUint64(spanID[:], tracing.SpanID)
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	return trace.ContextWithRemoteSpanContext(base, spanContext)
}

func (c *actorContext) Context() context.Context {
	if c == nil || c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}
func (c *actorContext) Deadline() (time.Time, bool) { return c.Context().Deadline() }
func (c *actorContext) Done() <-chan struct{}       { return c.Context().Done() }
func (c *actorContext) Err() error                  { return c.Context().Err() }
func (c *actorContext) Value(key any) any           { return c.Context().Value(key) }
func (c *actorContext) Sender() gxyactor.PID {
	if c == nil {
		return gxyactor.PID{}
	}
	return c.sender
}
func (c *actorContext) Self() gxyactor.PID {
	if c == nil || c.adapter == nil || c.process == nil {
		return gxyactor.PID{}
	}
	return c.adapter.fromErgoPID(c.process.PID(), c.process.id)
}
func (c *actorContext) Stop(reason error) {
	if c == nil || c.adapter == nil || c.process == nil {
		return
	}
	c.process.host.SetStopReason(reason)
	_ = c.adapter.Stop(c.Self())
}
func (c *actorContext) Watch(pid gxyactor.PID) {
	if c == nil || c.process == nil {
		return
	}
	if raw, err := c.adapter.toErgoPID(pid); err == nil {
		_ = c.process.MonitorPID(raw)
	}
}
func (c *actorContext) Unwatch(pid gxyactor.PID) {
	if c == nil || c.process == nil {
		return
	}
	if raw, err := c.adapter.toErgoPID(pid); err == nil {
		_ = c.process.DemonitorPID(raw)
	}
}
func (c *actorContext) Children() []gxyactor.PID { return nil }
func (c *actorContext) Timer() *gxyactor.ActorTimer {
	if c == nil || c.process == nil {
		return nil
	}
	return c.process.host.Timer()
}
func (c *actorContext) Span() trace.Span { return trace.SpanFromContext(c) }
func (c *actorContext) SetLogValue(key string, value any) {
	if c == nil || c.process == nil {
		return
	}
	c.ctx = c.process.host.SetLogValue(c.Context(), key, value)
}
func (c *actorContext) AddMsgHandler(handler any, prefix ...string) []*gxyutil.MethodMeta {
	if c == nil || c.process == nil {
		return nil
	}
	return c.process.host.AddMsgHandler(handler, prefix...)
}
func (c *actorContext) AutoHandleMsg(message any) (any, error) {
	if c == nil || c.process == nil {
		return nil, gen.ErrProcessUnknown
	}
	return c.process.host.AutoHandleMsg(c, message)
}
func (c *actorContext) Respond(message any, responseErr ...error) error {
	if c == nil || c.adapter == nil {
		return gen.ErrProcessUnknown
	}
	if c.request == nil {
		return nil
	}
	var err error
	if len(responseErr) > 0 {
		err = responseErr[0]
	}
	return c.adapter.Respond(c, c.request, message, err)
}
func (c *actorContext) MessageHeader() map[string]string { return nil }
func (c *actorContext) Runtime() gxyactor.Runtime {
	if c == nil {
		return nil
	}
	return c.adapter
}
