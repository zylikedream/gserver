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

	"github.com/asynkron/protoactor-go/actor"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/util/gutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// 类型别名 - 抽象层，隐藏具体实现但保持兼容性
type (
	PID = *actor.PID // 进程ID
)

type ActorTimerMsg gxytimer.TimerActiveInfo

type ActorInitMsg struct {
}

type ActorContext struct {
	actor.Context
	InitArgs []any
}

type ActorProducer func() IActor
type IActor interface {
	Init(ctx context.Context, args []any) error
	DelayInit(ctx context.Context) error
	Terminate(ctx context.Context, err error)
	Timer() *ActorTimer
	Self() PID
	HandleMessage(ctx context.Context, msg any) error
	actor.Actor
}

type ActorBase struct {
	timer      *ActorTimer
	self       PID
	Actx       actor.Context
	ctx        context.Context
	actor      IActor
	stopErr    error
	msgHandler *gxyutil.MsgHandler
	actorKind  string
	span       trace.Span
}

func NewActorBase(ctx context.Context, actor IActor, actorKind string) *ActorBase {
	return &ActorBase{
		ctx:        ctx,
		actor:      actor,
		actorKind:  actorKind,
		msgHandler: gxyutil.NewMsgHandler(),
	}
}

func (a *ActorBase) Span() trace.Span {
	return a.span
}

func (a *ActorBase) ActorKind() string {
	return a.actorKind
}

func (a *ActorBase) Receive(actx actor.Context) {
	a.Actx = actx
	gutil.TryCatch(a.ctx, func(ctx context.Context) {
		if err := a.doReceive(actx); err != nil {
			gxylog.Error(a.ctx, "actor error", gxylog.Err(err))
			a.Stop(err)
		}
	}, func(ctx context.Context, exception error) {
		gxylog.Error(a.ctx, "actor internal error", gxylog.Err(exception))
		a.Stop(exception)
	})
}

func (a *ActorBase) doReceive(ctx actor.Context) error {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		a.self = ctx.Self()
		gxymetrics.ActorActiveCount.WithLabelValues(a.ActorKind()).Inc()
		a.timer = NewActorTimer(a.self)
		var initArgs []any
		if actorCtx, ok := ctx.(*ActorContext); ok {
			initArgs = actorCtx.InitArgs
		}
		if err := a.actor.Init(a.ctx, initArgs); err != nil {
			return gerror.Wrap(err, "init actor error")
		}
		_ = LocalSend(a.ctx, a.self, &ActorInitMsg{})
	case *ActorInitMsg:
		a.msgHandler.AddHandler(a.actor)
		if err := a.actor.DelayInit(a.ctx); err != nil {
			return gerror.Wrap(err, "delay init actor error")
		}
	case ActorTimerMsg:
		err := gutil.Try(a.ctx, func(ctx context.Context) {
			a.timer.Active(a.ctx, msg)
		})
		if err != nil {
			gxylog.Error(a.ctx, "timer active error", gxylog.Str("timer", msg.Name), gxylog.Err(err))
		}
	case *pb.ActorStop:
		a.Stop(errors.New(msg.Reason))
	case *actor.Stopping:
		return nil
	case *actor.Stopped:
		gxymetrics.ActorActiveCount.WithLabelValues(a.ActorKind()).Dec()
		a.timer.Stop(a.ctx)
		a.actor.Terminate(a.ctx, a.stopErr)
	case actor.AutoRespond:
		// Touch etc. — protoactor handles the response automatically
	case IUnspanMessage:
		return a.handleMessage(msg)
	default:
		span := a.initSpan(msg)
		a.span = span
		savedCtx := a.ctx
		a.ctx = trace.ContextWithSpan(a.ctx, span)
		defer func() {
			span.End()
			a.ctx = savedCtx
		}()
		if err := a.handleMessage(msg); err != nil {
			span.RecordError(err)
			return err
		}
	}
	return nil
}

func (a *ActorBase) initSpan(msg any) trace.Span {
	header := a.Actx.MessageHeader()
	headerMap := map[string]string{}
	if header != nil {
		headerMap = header.ToMap()
	}
	carrier := readonlyHeaderCarrier{headerMap}
	extCtx := otel.GetTextMapPropagator().Extract(a.ctx, carrier)
	_, span := otel.Tracer("gserver/actor").Start(extCtx, fmt.Sprintf("%T", msg))
	span.SetAttributes(attribute.String("actor_kind", a.ActorKind()))
	span.SetAttributes(attribute.String("msg", gxyutil.FormatObject(msg)))
	// sc := span.SpanContext()
	// if sc.IsValid() && sc.TraceID().IsValid() {
	// 	parentSc := trace.SpanContextFromContext(extCtx)
	// 	gxylog.Debug(a.ctx, "trace",
	// 		gxylog.Str("trace_id", sc.TraceID().String()),
	// 		gxylog.Str("msg_type", fmt.Sprintf("%T", msg)),
	// 		gxylog.Bool("propagated", parentSc.IsValid()),
	// 	)
	// }
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
		_ = Respond(ctx, a.Actx, &pb.ActorError{
			Reason: err.Error(),
		})
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
	gxylog.Debug(ctx, "handle msg end, msg",
		gxylog.Str("payload", gxyutil.FormatObject(msg)),
		gxylog.Str("result", gxyutil.FormatObject(result)),
		gxylog.Err(err),
		gxylog.Num("cost", time.Since(tm).Milliseconds()))
	return result, err
}

func (a *ActorBase) DoCallMsgHandler(ctx context.Context, msg any) (any, error) {
	result, err := a.CallHandlerMsg(ctx, msg)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (a *ActorBase) Stop(err error) {
	a.stopErr = err
	a.Actx.Stop(a.self)
}

func (a *ActorBase) Timer() *ActorTimer {
	return a.timer
}

func (a *ActorBase) Self() PID {
	return a.self
}

func (a *ActorBase) Init(ctx context.Context, args []any) error {
	return nil
}

func (a *ActorBase) DelayInit(ctx context.Context) error {
	return nil
}

func (a *ActorBase) Terminate(ctx context.Context, err error) {
}

// 发送请求里带了sender，所以接收方可以调用respond回应消息

func (a *ActorBase) Sender() PID {
	return a.Actx.Sender()
}

// Context returns the actor's context with trace span enrichment.
func (a *ActorBase) Context() context.Context {
	return a.ctx
}

func (a *ActorBase) SetLogValue(key string, val any) *ActorBase {
	a.ctx = gxylog.WithValue(a.ctx, key, val)
	return a
}

func (a *ActorBase) AddMsgHandler(handler any, prefix ...string) []*gxyutil.MethodMeta {
	return a.msgHandler.AddHandler(handler, prefix...)
}

func (a *ActorBase) CallHandlerMsg(ctx context.Context, msg any) (any, error) {
	return a.msgHandler.CallWithMsg(ctx, msg)
}

func ContextDecorator(args ...any) actor.ContextDecorator {
	return func(next actor.ContextDecoratorFunc) actor.ContextDecoratorFunc {
		return func(ctx actor.Context) actor.Context {
			return &ActorContext{
				Context:  ctx,
				InitArgs: args,
			}
		}
	}
}

// readonlyHeaderCarrier adapts actor.ReadonlyMessageHeader to propagation.TextMapCarrier
// for OpenTelemetry trace context extraction. Set is a no-op since the header is read-only.
type readonlyHeaderCarrier struct {
	mp map[string]string
}

func (c readonlyHeaderCarrier) Set(key, value string) {
	c.mp[key] = value
}

func (c readonlyHeaderCarrier) Get(key string) string {
	return c.mp[key]
}

func (c readonlyHeaderCarrier) Keys() []string {
	return gutil.Keys(c.mp)
}

type IUnspanMessage interface {
	Unspan()
}

type unspanMessage struct {
}

func (u *unspanMessage) Unspan() {
}
