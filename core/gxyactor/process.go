package gxyactor

import (
	"context"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxyutil"
	"gserver/protocol/pb"

	"github.com/cockroachdb/errors"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/util/gutil"
)

// ActorProcess owns the runtime concerns shared by every business Actor.
// Runtime adapters drive it directly from their native lifecycle callbacks.
type ActorProcess struct {
	actor      IActor
	kind       string
	baseCtx    context.Context
	self       PID
	timer      *ActorTimer
	msgHandler *gxyutil.MsgHandler
	stopErr    error
	active     bool
}

func NewActorProcess(kind string, actor IActor) *ActorProcess {
	return &ActorProcess{
		actor:      actor,
		kind:       kind,
		baseCtx:    gxylog.NewContext(context.Background(), kind),
		msgHandler: gxyutil.NewMsgHandler(),
	}
}

// Context builds the callback context used by the adapter. Actor state is
// callback-confined, so the persistent log context needs no synchronization.
func (p *ActorProcess) Context(incoming context.Context, runtime Runtime) context.Context {
	base := p.baseCtx
	if base == nil {
		base = context.Background()
	}
	if incoming != nil {
		base = mergedContext{primary: incoming, fallback: base}
	}
	if runtime != nil {
		base = context.WithValue(base, runtimeContextKey{}, runtime)
	}
	return base
}

func (p *ActorProcess) Init(ctx ActorContext, args []any) error {
	if p == nil || p.actor == nil {
		return errors.New("actor process has no behavior")
	}
	p.self = ctx.Self()
	p.timer = NewActorTimer(p.self)
	p.active = true
	gxymetrics.ActorActiveCount.WithLabelValues(p.kind).Inc()

	if err := p.invoke(ctx, func() error { return p.actor.Init(ctx, args) }); err != nil {
		return gerror.Wrap(err, "init actor error")
	}
	p.msgHandler.AddHandler(p.actor)
	if err := p.invoke(ctx, func() error { return p.actor.DelayInit(ctx) }); err != nil {
		return gerror.Wrap(err, "delay init actor error")
	}
	return nil
}

func (p *ActorProcess) HandleMessage(ctx ActorContext, message any) error {
	if p == nil || p.actor == nil {
		return errors.New("actor process has no behavior")
	}
	switch msg := message.(type) {
	case ActorTimerMsg:
		if p.timer == nil {
			return errors.New("actor timer is not initialized")
		}
		return p.invoke(ctx, func() error { return p.timer.Active(ctx, msg) })
	case *pb.ActorStop:
		if msg != nil {
			ctx.Stop(errors.New(msg.Reason))
		}
		return nil
	default:
		return p.handleMessage(ctx, msg)
	}
}

func (p *ActorProcess) HandleCall(ctx ActorContext, message any) (any, error) {
	if p == nil || p.actor == nil {
		return nil, errors.New("actor process has no behavior")
	}
	return p.callMsgHandler(ctx, message)
}

func (p *ActorProcess) Terminate(ctx ActorContext, reason error) {
	if p == nil || p.actor == nil {
		return
	}
	if p.stopErr != nil {
		reason = p.stopErr
	}
	if p.timer != nil {
		p.timer.Stop(ctx)
	}
	p.actor.Terminate(ctx, reason)
	if p.active {
		p.active = false
		gxymetrics.ActorActiveCount.WithLabelValues(p.kind).Dec()
	}
}

func (p *ActorProcess) SetStopReason(reason error) {
	if reason != nil {
		p.stopErr = reason
	}
}

func (p *ActorProcess) Timer() *ActorTimer { return p.timer }

func (p *ActorProcess) SetLogValue(ctx context.Context, key string, value any) context.Context {
	p.baseCtx = gxylog.WithValue(p.baseCtx, key, value)
	return gxylog.WithValue(ctx, key, value)
}

func (p *ActorProcess) AddMsgHandler(handler any, prefix ...string) []*gxyutil.MethodMeta {
	return p.msgHandler.AddHandler(handler, prefix...)
}

func (p *ActorProcess) AutoHandleMsg(ctx ActorContext, message any) (any, error) {
	response, err := p.callMsgHandler(ctx, message)
	if err != nil {
		p.logError(ctx, "handle rpc msg failed", err)
		_ = ctx.Respond(nil, err)
		return nil, err
	}
	if response != nil {
		if err := ctx.Respond(response); err != nil {
			return nil, err
		}
	}
	return response, nil
}

func (p *ActorProcess) invoke(ctx context.Context, callback func() error) (err error) {
	gutil.TryCatch(ctx, func(context.Context) {
		err = callback()
	}, func(_ context.Context, exception error) {
		err = exception
	})
	if err != nil {
		p.logError(ctx, "actor callback failed", err)
	}
	return err
}

func (p *ActorProcess) handleMessage(ctx ActorContext, message any) error {
	start := time.Now()
	if err := p.invoke(ctx, func() error { return p.actor.HandleMessage(ctx, message) }); err != nil {
		return err
	}
	gxymetrics.ActorMessages.WithLabelValues(p.kind).Inc()
	gxymetrics.ActorMessageDuration.WithLabelValues(p.kind).Observe(time.Since(start).Seconds())
	return nil
}

func (p *ActorProcess) callMsgHandler(ctx ActorContext, message any) (any, error) {
	start := time.Now()
	gxylog.Debug(ctx, "handle msg start, msg", gxylog.Str("payload", gxyutil.FormatObject(message)))
	result, err := p.msgHandler.CallWithMsg(ctx, message)
	gxylog.Debug(ctx, "handle msg end, msg", gxylog.Str("payload", gxyutil.FormatObject(message)), gxylog.Str("result", gxyutil.FormatObject(result)), gxylog.Err(err), gxylog.Num("cost", time.Since(start).Milliseconds()))
	return result, err
}

func (p *ActorProcess) logError(ctx context.Context, message string, err error) {
	gxylog.Error(ctx, message,
		gxylog.Str("actor_kind", p.kind),
		gxylog.Str("node", p.self.Node),
		gxylog.Str("actor_id", p.self.ID),
		gxylog.Err(err))
}

type mergedContext struct {
	primary  context.Context
	fallback context.Context
}

func (c mergedContext) Deadline() (time.Time, bool) {
	if deadline, ok := c.primary.Deadline(); ok {
		return deadline, true
	}
	return c.fallback.Deadline()
}
func (c mergedContext) Done() <-chan struct{} {
	if done := c.primary.Done(); done != nil {
		return done
	}
	return c.fallback.Done()
}
func (c mergedContext) Err() error {
	if err := c.primary.Err(); err != nil {
		return err
	}
	return c.fallback.Err()
}
func (c mergedContext) Value(key any) any {
	if value := c.primary.Value(key); value != nil {
		return value
	}
	return c.fallback.Value(key)
}
