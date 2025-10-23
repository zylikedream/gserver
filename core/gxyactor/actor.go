package gxyactor

import (
	"context"
	"gserver/core/gxylog"
	"gserver/core/gxytimer"
	"gserver/protocol/pb"
	"gserver/util"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/gutil"
)

// 类型别名 - 抽象层，隐藏具体实现但保持兼容性
type (
	PID = *actor.PID // 进程ID
)

type ActorTimerMsg gxytimer.TimerActiveInfo

type ActorInitMsg struct {
}

type ActorInternalError struct {
	err error
}

type GrainContext struct {
	actor.Context
	ID   string
	Kind string
}

type GrainProducer func() IGrain
type IActor interface {
	Init(ctx context.Context) error
	DelayInit(ctx context.Context) error
	HandleMessage(ctx context.Context, msg any) error
	Terminate(ctx context.Context, err error)
	Timer() *ActorTimer
	Self() PID
	actor.Actor
}

type IGrain interface {
	IActor
	GrainID() string
}

type ActorBase struct {
	timer      *ActorTimer
	self       PID
	Actx       actor.Context
	ctx        context.Context
	actor      IActor
	stopErr    error
	msgHandler *util.MsgHandler
}

func NewActorBasse(ctx context.Context, actor IActor) *ActorBase {
	return &ActorBase{
		ctx:        ctx,
		actor:      actor,
		msgHandler: util.NewMsgHandler(),
	}
}

func (a *ActorBase) Receive(actx actor.Context) {
	a.Actx = actx
	gutil.TryCatch(a.ctx, func(ctx context.Context) {
		if err := a.doReceive(actx); err != nil {
			glog.Errorf(a.ctx, "grain error, %+v", err)
			a.Stop(err)
		}
	}, func(ctx context.Context, exception error) {
		glog.Errorf(a.ctx, "role internal error, %+v", exception)
		a.Stop(exception)
	})
}

func (a *ActorBase) doReceive(ctx actor.Context) error {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		a.self = ctx.Self()
		a.timer = NewActorTimer(a.self)
		if err := a.actor.Init(a.ctx); err != nil {
			return gerror.Wrap(err, "init actor error")
		}
		a.SendSelf(&ActorInitMsg{})
	case *ActorInitMsg:
		a.msgHandler.AddHandler(a.actor)
		if err := a.actor.DelayInit(a.ctx); err != nil {
			return gerror.Wrap(err, "delay init actor error")
		}
	case ActorTimerMsg:
		a.timer.Active(a.ctx, msg)
	case *pb.ApiMsg:
		a.msgHandler.CallWithMsg(a.ctx, msg.Msg)
	case *pb.ActorStop:
		a.Stop(gerror.New(msg.Reason))
	case *actor.Stopped:
		a.timer.CancelAll()
		a.actor.Terminate(a.ctx, a.stopErr)
	default:
		return a.actor.HandleMessage(a.ctx, ctx.Message())
	}
	return nil
}

func (a *ActorBase) Stop(err error) {
	a.stopErr = err
	a.Actx.Stop(a.self)
}

func (a *ActorBase) SendSelf(msg any) {
	a.Send(a.self, msg)
}

func (a *ActorBase) Sender() PID {
	return a.Actx.Sender()
}

func (a *ActorBase) Send(pid PID, msg any) {
	a.Actx.Send(pid, msg)
}

func (a *ActorBase) Timer() *ActorTimer {
	return a.timer
}

func (a *ActorBase) Self() PID {
	return a.self
}

func (a *ActorBase) DelayInit(ctx context.Context) error {
	return nil
}

func (a *ActorBase) Respond(msg any) {
	if a.Actx.Sender() == nil {
		glog.Infof(a.ctx, "respond sender is nil, msg: %v", msg)
		return
	}
	a.Actx.Respond(msg)
}

func (a *ActorBase) Request(pid PID, msg any) {
	a.Actx.Request(pid, msg)
}

func (a *ActorBase) SpawnNamed(props *actor.Props, name string) (PID, error) {
	return a.Actx.SpawnNamed(props, name)
}

func (a *ActorBase) SetLogValue(key string, val any) *ActorBase {
	a.ctx = gxylog.WithValue(a.ctx, key, val)
	return a
}

func (a *ActorBase) AddMsgHandler(handler any) {
	a.msgHandler.AddHandler(handler)
}

func (a *ActorBase) CallHandlerMsg(ctx context.Context, msg any) (any, error) {
	return a.msgHandler.CallWithMsg(ctx, msg)
}

type GrainBase struct {
	grainID string
	*ActorBase
	grain IGrain
}

func NewGrainBase(ctx context.Context, grain IGrain) *GrainBase {
	base := &GrainBase{
		grain: grain,
	}
	base.ActorBase = NewActorBasse(ctx, grain)
	return base
}

func (g *GrainBase) GrainID() string {
	return g.grainID
}

func (g *GrainBase) Receive(ctx actor.Context) {
	switch ctx.Message().(type) {
	case *actor.Started:
		grainCtx := ctx.(*GrainContext)
		g.grainID = grainCtx.ID
	}
	g.ActorBase.Receive(ctx)
}
