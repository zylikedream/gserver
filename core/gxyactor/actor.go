package gxyactor

import (
	"context"
	"fmt"
	"time"

	"github.com/cockroachdb/errors"

	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxytimer"
	"gserver/core/gxyutil"
	"gserver/protocol/pb"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/util/gutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type ActorTimerMsg gxytimer.TimerActiveInfo

type ActorInitMsg struct{}

type ActorProducer func() IActor

type IActor interface {
	Init(context.Context, []any) error
	DelayInit(context.Context) error
	Terminate(context.Context, error)
	Timer() *ActorTimer
	Self() PID
	HandleMessage(context.Context, any) error
}

type ActorContextDecorator func(ActorContext) ActorContext

type ActorBase struct {
	timer      *ActorTimer
	self       PID
	Actx       ActorContext
	ctx        context.Context
	actor      IActor
	stopErr    error
	msgHandler *gxyutil.MsgHandler
	actorKind  string
	span       trace.Span
}

func NewActorBase(ctx context.Context, actor IActor, actorKind string) *ActorBase {
	return &ActorBase{ctx: ctx, actor: actor, actorKind: actorKind, msgHandler: gxyutil.NewMsgHandler()}
}

func (a *ActorBase) Span() trace.Span { return a.span }
func (a *ActorBase) ActorKind() string { return a.actorKind }

// Receive is the sole adapter entrypoint. Runtime-specific contexts never
// cross this public method; adapters provide the small ActorContext contract.
func (a *ActorBase) Receive(actx ActorContext) {
	a.Actx = actx
	gutil.TryCatch(a.ctx, func(context.Context) {
		if err := a.doReceive(actx); err != nil {
			gxylog.Error(a.ctx, "actor error", gxylog.Err(err))
			a.Stop(err)
		}
	}, func(_ context.Context, exception error) {
		gxylog.Error(a.ctx, "actor internal error", gxylog.Err(exception))
		a.Stop(exception)
	})
}

func (a *ActorBase) doReceive(ctx ActorContext) error {
	switch msg := ctx.Message().(type) {
	case ActorStartedMessage:
		a.self = msg.Self
		gxymetrics.ActorActiveCount.WithLabelValues(a.ActorKind()).Inc()
		a.timer = NewActorTimer(a.self)
		if err := a.actor.Init(a.ctx, msg.InitArgs); err != nil {
			return gerror.Wrap(err, "init actor error")
		}
		_ = LocalSend(a.ctx, a.self, ActorInitMsg{})
	case *ActorStartedMessage:
		if msg != nil {
			return a.doReceiveWithStarted(ctx, *msg)
		}
	case ActorInitMsg, *ActorInitMsg:
		a.msgHandler.AddHandler(a.actor)
		if err := a.actor.DelayInit(a.ctx); err != nil {
			return gerror.Wrap(err, "delay init actor error")
		}
	case ActorTimerMsg:
		if err := gutil.Try(a.ctx, func(context.Context) { a.timer.Active(a.ctx, msg) }); err != nil {
			gxylog.Error(a.ctx, "timer active error", gxylog.Str("timer", msg.Name), gxylog.Err(err))
		}
	case *pb.ActorStop:
		a.Stop(errors.New(msg.Reason))
	case LifecycleMessage:
		switch msg {
		case ActorStopping:
			return nil
		case ActorStopped:
			a.terminate(nil)
		case ActorAutoRespond:
			return nil
		}
	case ActorStoppedMessage:
		a.terminate(msg.Err)
	case *ActorStoppedMessage:
		if msg != nil {
			a.terminate(msg.Err)
		}
	case IUnspanMessage:
		return a.handleMessage(msg)
	default:
		span := a.initSpan(msg)
		a.span = span
		savedCtx := a.ctx
		a.ctx = trace.ContextWithSpan(a.ctx, span)
		defer func() { span.End(); a.ctx = savedCtx }()
		if err := a.handleMessage(msg); err != nil {
			span.RecordError(err)
			return err
		}
	}
	return nil
}

func (a *ActorBase) doReceiveWithStarted(ctx ActorContext, msg ActorStartedMessage) error {
	a.self = msg.Self
	gxymetrics.ActorActiveCount.WithLabelValues(a.ActorKind()).Inc()
	a.timer = NewActorTimer(a.self)
	if err := a.actor.Init(a.ctx, msg.InitArgs); err != nil {
		return gerror.Wrap(err, "init actor error")
	}
	_ = LocalSend(a.ctx, a.self, ActorInitMsg{})
	return nil
}

func (a *ActorBase) terminate(err error) {
	gxymetrics.ActorActiveCount.WithLabelValues(a.ActorKind()).Dec()
	if a.timer != nil {
		a.timer.Stop(a.ctx)
	}
	a.actor.Terminate(a.ctx, err)
}

func (a *ActorBase) initSpan(msg any) trace.Span {
	headerMap := map[string]string{}
	if a.Actx != nil && a.Actx.MessageHeader() != nil {
		headerMap = a.Actx.MessageHeader()
	}
	extCtx := otel.GetTextMapPropagator().Extract(a.ctx, readonlyHeaderCarrier{headerMap})
	_, span := otel.Tracer("gserver/actor").Start(extCtx, fmt.Sprintf("%T", msg))
	span.SetAttributes(attribute.String("actor_kind", a.ActorKind()))
	span.SetAttributes(attribute.String("msg", gxyutil.FormatObject(msg)))
	return span
}

func (a *ActorBase) handleMessage(msg any) error {
	start := time.Now()
	if err := a.actor.HandleMessage(a.ctx, msg); err != nil {
		gxylog.Error(a.ctx, "handle msg failed", gxylog.Any("payload", msg), gxylog.Err(err))
		return err
	}
	gxymetrics.ActorMessages.WithLabelValues(a.ActorKind()).Inc()
	gxymetrics.ActorMessageDuration.WithLabelValues(a.ActorKind()).Observe(time.Since(start).Seconds())
	return nil
}

func (a *ActorBase) AutoHandleMsg(ctx context.Context, msg any) (any, error) {
	rsp, err := a.callMsgHandler(a.ctx, msg)
	if err != nil {
		gxylog.Error(a.ctx, "handle rpc msg failed", gxylog.Any("payload", msg), gxylog.Err(err))
		_ = Respond(ctx, a.Actx, &pb.ActorError{Reason: err.Error()})
		return nil, nil
	}
	if rsp != nil {
		_ = Respond(ctx, a.Actx, rsp)
	}
	return rsp, nil
}

func (a *ActorBase) callMsgHandler(ctx context.Context, msg any) (any, error) {
	tm := time.Now()
	gxylog.Debug(ctx, "handle msg start, msg", gxylog.Str("payload", gxyutil.FormatObject(msg)))
	result, err := a.DoCallMsgHandler(ctx, msg)
	gxylog.Debug(ctx, "handle msg end, msg", gxylog.Str("payload", gxyutil.FormatObject(msg)), gxylog.Str("result", gxyutil.FormatObject(result)), gxylog.Err(err), gxylog.Num("cost", time.Since(tm).Milliseconds()))
	return result, err
}

func (a *ActorBase) DoCallMsgHandler(ctx context.Context, msg any) (any, error) {
	return a.CallHandlerMsg(ctx, msg)
}

func (a *ActorBase) Stop(err error) {
	a.stopErr = err
	if a.Actx != nil {
		a.Actx.Stop(a.self)
	}
}

func (a *ActorBase) Timer() *ActorTimer { return a.timer }
func (a *ActorBase) Self() PID { return a.self }
func (a *ActorBase) Init(context.Context, []any) error { return nil }
func (a *ActorBase) DelayInit(context.Context) error { return nil }
func (a *ActorBase) Terminate(context.Context, error) {}
func (a *ActorBase) Sender() PID {
	if a.Actx == nil { return PID{} }
	return a.Actx.Sender()
}
func (a *ActorBase) Context() context.Context { return a.ctx }
func (a *ActorBase) SetLogValue(key string, val any) *ActorBase { a.ctx = gxylog.WithValue(a.ctx, key, val); return a }
func (a *ActorBase) AddMsgHandler(handler any, prefix ...string) []*gxyutil.MethodMeta { return a.msgHandler.AddHandler(handler, prefix...) }
func (a *ActorBase) CallHandlerMsg(ctx context.Context, msg any) (any, error) { return a.msgHandler.CallWithMsg(ctx, msg) }

func ContextDecorator(args ...any) ActorContextDecorator {
	return func(ctx ActorContext) ActorContext {
		return &decoratedActorContext{ActorContext: ctx, InitArgs: args}
	}
}

type decoratedActorContext struct { ActorContext; InitArgs []any }

type readonlyHeaderCarrier struct { mp map[string]string }
func (c readonlyHeaderCarrier) Set(key, value string) { c.mp[key] = value }
func (c readonlyHeaderCarrier) Get(key string) string { return c.mp[key] }
func (c readonlyHeaderCarrier) Keys() []string { return gutil.Keys(c.mp) }

type IUnspanMessage interface { Unspan() }
type unspanMessage struct{}
func (*unspanMessage) Unspan() {}
