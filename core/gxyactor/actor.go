package gxyactor

import (
	"context"
	"fmt"
	"sync"
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

	lifecycleMu sync.Mutex
	initErr     error
	ready       bool
	active      bool
	termOnce    sync.Once
	receiveErr  error
}

func (a *ActorBase) LifecycleError() error {
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
	if a.initErr != nil {
		return a.initErr
	}
	return a.receiveErr
}

func (a *ActorBase) setReceiveError(err error) {
	a.lifecycleMu.Lock()
	a.receiveErr = err
	a.lifecycleMu.Unlock()
}

func (a *ActorBase) callbackContext(actx ActorContext) context.Context {
	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	if provider, ok := actx.(interface{ Context() context.Context }); ok {
		incoming := provider.Context()
		if incoming != nil {
			base = mergedContext{primary: incoming, fallback: base}
		}
	}
	if provider, ok := actx.(interface{ Runtime() Runtime }); ok {
		if runtime := provider.Runtime(); runtime != nil {
			base = context.WithValue(base, runtimeContextKey{}, runtime)
		}
	}
	return base
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

func NewActorBase(ctx context.Context, actor IActor, actorKind string) *ActorBase {
	return &ActorBase{ctx: ctx, actor: actor, actorKind: actorKind, msgHandler: gxyutil.NewMsgHandler()}
}

func (a *ActorBase) Span() trace.Span  { return a.span }
func (a *ActorBase) ActorKind() string { return a.actorKind }

// Receive is the sole adapter entrypoint. Runtime-specific contexts never
// cross this public method; adapters provide the small ActorContext contract.
func (a *ActorBase) Receive(actx ActorContext) {
	a.Actx = actx
	a.ctx = a.callbackContext(actx)
	gutil.TryCatch(a.ctx, func(context.Context) {
		if err := a.doReceive(actx); err != nil {
			a.setReceiveError(err)
			a.logError("actor error", err)
			a.Stop(err)
		}
	}, func(_ context.Context, exception error) {
		a.setReceiveError(exception)
		a.logError("actor internal error", exception)
		a.Stop(exception)
	})
}

func (a *ActorBase) logError(message string, err error) {
	gxylog.Error(a.ctx, message,
		gxylog.Str("actor_kind", a.ActorKind()),
		gxylog.Str("node", a.self.Node),
		gxylog.Str("actor_id", a.self.ID),
		gxylog.Err(err))
}

func (a *ActorBase) initialize(ctx ActorContext, msg ActorStartedMessage) error {
	a.self = msg.Self
	a.lifecycleMu.Lock()
	if a.active || a.ready || a.initErr != nil {
		a.lifecycleMu.Unlock()
		return nil
	}
	a.active = true
	a.lifecycleMu.Unlock()
	gxymetrics.ActorActiveCount.WithLabelValues(a.ActorKind()).Inc()
	a.timer = NewActorTimer(a.self)
	if err := a.actor.Init(a.ctx, msg.InitArgs); err != nil {
		err = gerror.Wrap(err, "init actor error")
		a.lifecycleMu.Lock()
		a.initErr = err
		a.lifecycleMu.Unlock()
		return err
	}
	a.msgHandler.AddHandler(a.actor)
	if err := a.actor.DelayInit(a.ctx); err != nil {
		err = gerror.Wrap(err, "delay init actor error")
		a.lifecycleMu.Lock()
		a.initErr = err
		a.lifecycleMu.Unlock()
		return err
	}
	a.lifecycleMu.Lock()
	a.ready = true
	a.lifecycleMu.Unlock()
	return nil
}

func (a *ActorBase) doReceive(ctx ActorContext) error {
	switch msg := ctx.Message().(type) {
	case ActorStartedMessage:
		return a.initialize(ctx, msg)
	case *ActorStartedMessage:
		if msg != nil {
			return a.initialize(ctx, *msg)
		}
	case ActorInitMsg, *ActorInitMsg:
		// Kept as a compatibility marker for older adapters. New adapters
		// complete initialization in ProcessInit before mailbox delivery.
		return nil
	case ActorTimerMsg:
		a.lifecycleMu.Lock()
		ready := a.ready
		a.lifecycleMu.Unlock()
		if !ready {
			return gerror.New("actor is not ready")
		}
		if a.timer == nil {
			return gerror.New("actor timer is not initialized")
		}
		if err := a.timer.Active(a.ctx, msg); err != nil {
			return gerror.Wrap(err, "timer active error")
		}
	case *pb.ActorStop:
		if msg != nil {
			a.Stop(errors.New(msg.Reason))
		}
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
		a.lifecycleMu.Lock()
		ready := a.ready
		a.lifecycleMu.Unlock()
		if !ready {
			return gerror.New("actor is not ready")
		}
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

func (a *ActorBase) terminate(err error) {
	a.termOnce.Do(func() {
		if err == nil {
			err = a.stopErr
		}
		if a.timer != nil {
			a.timer.Stop(a.ctx)
		}
		a.actor.Terminate(a.ctx, err)
		a.lifecycleMu.Lock()
		active := a.active
		a.active = false
		a.ready = false
		a.lifecycleMu.Unlock()
		if active {
			gxymetrics.ActorActiveCount.WithLabelValues(a.ActorKind()).Dec()
		}
	})
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
		a.logError("handle msg failed", err)
		return err
	}
	gxymetrics.ActorMessages.WithLabelValues(a.ActorKind()).Inc()
	gxymetrics.ActorMessageDuration.WithLabelValues(a.ActorKind()).Observe(time.Since(start).Seconds())
	return nil
}

func (a *ActorBase) AutoHandleMsg(ctx context.Context, msg any) (any, error) {
	if ctx == nil {
		ctx = a.ctx
	}
	rsp, err := a.callMsgHandler(ctx, msg)
	request := a.requestHandle()
	if err != nil {
		a.logError("handle rpc msg failed", err)
		if request != nil {
			_ = Respond(ctx, request, nil, err)
		}
		return nil, err
	}
	if rsp != nil && request != nil {
		if err := Respond(ctx, request, rsp); err != nil {
			return nil, err
		}
	}
	return rsp, nil
}

func (a *ActorBase) requestHandle() Request {
	if a.Actx == nil {
		return nil
	}
	if provider, ok := a.Actx.(interface{ RequestHandle() Request }); ok {
		return provider.RequestHandle()
	}
	return nil
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
	if err != nil {
		a.stopErr = err
	}
	if a.Actx != nil {
		a.Actx.Stop(a.self)
	}
}

func (a *ActorBase) Timer() *ActorTimer                { return a.timer }
func (a *ActorBase) Self() PID                         { return a.self }
func (a *ActorBase) Init(context.Context, []any) error { return nil }
func (a *ActorBase) DelayInit(context.Context) error   { return nil }
func (a *ActorBase) Terminate(context.Context, error)  {}
func (a *ActorBase) Sender() PID {
	if a.Actx == nil {
		return PID{}
	}
	return a.Actx.Sender()
}
func (a *ActorBase) Context() context.Context { return a.ctx }
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

func ContextDecorator(args ...any) ActorContextDecorator {
	return func(ctx ActorContext) ActorContext {
		return &decoratedActorContext{ActorContext: ctx, InitArgs: args}
	}
}

type decoratedActorContext struct {
	ActorContext
	InitArgs []any
}

type readonlyHeaderCarrier struct{ mp map[string]string }

func (c readonlyHeaderCarrier) Set(key, value string) { c.mp[key] = value }
func (c readonlyHeaderCarrier) Get(key string) string { return c.mp[key] }
func (c readonlyHeaderCarrier) Keys() []string        { return gutil.Keys(c.mp) }

type IUnspanMessage interface{ Unspan() }
type unspanMessage struct{}

func (*unspanMessage) Unspan() {}
