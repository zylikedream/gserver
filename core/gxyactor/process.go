package gxyactor

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxyutil"
	"gserver/protocol/pb"

	"github.com/cockroachdb/errors"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/util/gutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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

func (p *ActorProcess) Init(actx ActorContext, args []any) error {
	if p == nil || p.actor == nil {
		return errors.New("actor process has no behavior")
	}
	ctx := p.callbackContext(actx)
	p.self = actx.Self()
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

func (p *ActorProcess) HandleMessage(actx ActorContext, message any) error {
	if p == nil || p.actor == nil {
		return errors.New("actor process has no behavior")
	}
	ctx := p.callbackContext(actx)
	switch msg := message.(type) {
	case ActorTimerMsg:
		if p.timer == nil {
			return errors.New("actor timer is not initialized")
		}
		return p.invoke(ctx, func() error { return p.timer.Active(ctx, msg) })
	case *pb.ActorStop:
		if msg != nil {
			p.Stop(actx, errors.New(msg.Reason))
		}
		return nil
	case IUnspanMessage:
		return p.handleMessage(ctx, msg)
	default:
		ctx, span := p.startSpan(ctx, actx, msg)
		defer span.End()
		if err := p.handleMessage(ctx, msg); err != nil {
			span.RecordError(err)
			return err
		}
		return nil
	}
}

func (p *ActorProcess) HandleCall(actx ActorContext, message any) (any, error) {
	if p == nil || p.actor == nil {
		return nil, errors.New("actor process has no behavior")
	}
	ctx := p.callbackContext(actx)
	return p.callMsgHandler(ctx, message)
}

func (p *ActorProcess) Terminate(actx ActorContext, reason error) {
	if p == nil || p.actor == nil {
		return
	}
	ctx := p.callbackContext(actx)
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

func (p *ActorProcess) Stop(actx ActorContext, reason error) {
	if reason != nil {
		p.stopErr = reason
	}
	if actx != nil {
		actx.Stop(p.self)
	}
}

func (p *ActorProcess) Timer() *ActorTimer { return p.timer }
func (p *ActorProcess) Self() PID          { return p.self }
func (p *ActorProcess) Span(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

func (p *ActorProcess) SetLogValue(ctx context.Context, key string, value any) context.Context {
	p.baseCtx = gxylog.WithValue(p.baseCtx, key, value)
	return gxylog.WithValue(ctx, key, value)
}

func (p *ActorProcess) AddMsgHandler(handler any, prefix ...string) []*gxyutil.MethodMeta {
	return p.msgHandler.AddHandler(handler, prefix...)
}

func (p *ActorProcess) AutoHandleMsg(ctx context.Context, actx ActorContext, message any) (any, error) {
	response, err := p.callMsgHandler(ctx, message)
	request := requestFromActorContext(actx)
	if err != nil {
		p.logError(ctx, "handle rpc msg failed", err)
		if request != nil {
			_ = Respond(ctx, request, nil, err)
		}
		return nil, err
	}
	if response != nil && request != nil {
		if err := Respond(ctx, request, response); err != nil {
			return nil, err
		}
	}
	return response, nil
}

func (p *ActorProcess) callbackContext(actx ActorContext) context.Context {
	base := p.baseCtx
	if base == nil {
		base = context.Background()
	}
	if provider, ok := actx.(interface{ Context() context.Context }); ok {
		if incoming := provider.Context(); incoming != nil {
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

func (p *ActorProcess) handleMessage(ctx context.Context, message any) error {
	start := time.Now()
	if err := p.invoke(ctx, func() error { return p.actor.HandleMessage(ctx, message) }); err != nil {
		return err
	}
	gxymetrics.ActorMessages.WithLabelValues(p.kind).Inc()
	gxymetrics.ActorMessageDuration.WithLabelValues(p.kind).Observe(time.Since(start).Seconds())
	return nil
}

func (p *ActorProcess) callMsgHandler(ctx context.Context, message any) (any, error) {
	start := time.Now()
	gxylog.Debug(ctx, "handle msg start, msg", gxylog.Str("payload", gxyutil.FormatObject(message)))
	result, err := p.msgHandler.CallWithMsg(ctx, message)
	gxylog.Debug(ctx, "handle msg end, msg", gxylog.Str("payload", gxyutil.FormatObject(message)), gxylog.Str("result", gxyutil.FormatObject(result)), gxylog.Err(err), gxylog.Num("cost", time.Since(start).Milliseconds()))
	return result, err
}

func (p *ActorProcess) startSpan(ctx context.Context, actx ActorContext, message any) (context.Context, trace.Span) {
	headers := map[string]string{}
	if actx != nil && actx.MessageHeader() != nil {
		headers = actx.MessageHeader()
	}
	extracted := otel.GetTextMapPropagator().Extract(ctx, readonlyHeaderCarrier{mp: headers})
	ctx, span := otel.Tracer("gserver/actor").Start(extracted, fmt.Sprintf("%T", message))
	span.SetAttributes(attribute.String("actor_kind", p.kind))
	span.SetAttributes(attribute.String("msg", gxyutil.FormatObject(message)))
	return ctx, span
}

func (p *ActorProcess) logError(ctx context.Context, message string, err error) {
	gxylog.Error(ctx, message,
		gxylog.Str("actor_kind", p.kind),
		gxylog.Str("node", p.self.Node),
		gxylog.Str("actor_id", p.self.ID),
		gxylog.Err(err))
}

func requestFromActorContext(actx ActorContext) Request {
	if provider, ok := actx.(interface{ RequestHandle() Request }); ok {
		return provider.RequestHandle()
	}
	return nil
}
