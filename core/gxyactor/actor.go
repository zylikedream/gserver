package gxyactor

import (
	"context"
	"gserver/core/gxylog"
	"gserver/core/gxytimer"

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
	timer   *ActorTimer
	self    PID
	Actx    actor.Context
	ctx     context.Context
	actor   IActor
	stopErr error
}

func NewActorBasse(ctx context.Context, actor IActor) *ActorBase {
	return &ActorBase{
		ctx:   ctx,
		actor: actor,
	}
}

func (g *ActorBase) Receive(actx actor.Context) {
	g.Actx = actx
	gutil.TryCatch(g.ctx, func(ctx context.Context) {
		if err := g.doReceive(actx); err != nil {
			glog.Errorf(g.ctx, "grain error, %+v", err)
			g.Stop(err)
		}
	}, func(ctx context.Context, exception error) {
		glog.Errorf(g.ctx, "role internal error, %+v", exception)
		g.Stop(exception)
	})
}

func (g *ActorBase) doReceive(ctx actor.Context) error {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		g.self = ctx.Self()
		g.timer = NewActorTimer(g.self)
		if err := g.actor.Init(g.ctx); err != nil {
			return gerror.Wrap(err, "init actor error")
		}
		g.SendSelf(&ActorInitMsg{})
	case *ActorInitMsg:
		if err := g.actor.DelayInit(g.ctx); err != nil {
			return gerror.Wrap(err, "delay init actor error")
		}
	case ActorTimerMsg:
		g.timer.Active(g.ctx, msg)
	case *actor.Stopped:
		g.actor.Terminate(g.ctx, g.stopErr)
	default:
		return g.actor.HandleMessage(g.ctx, ctx.Message())
	}
	return nil
}

func (g *ActorBase) Stop(err error) {
	g.stopErr = err
	g.Actx.Stop(g.self)
}

func (g *ActorBase) SendSelf(msg any) {
	g.Send(g.self, msg)
}

func (g *ActorBase) Sender() PID {
	return g.Actx.Sender()
}

func (g *ActorBase) Send(pid PID, msg any) {
	g.Actx.Send(pid, msg)
}

func (g *ActorBase) Timer() *ActorTimer {
	return g.timer
}

func (g *ActorBase) Self() PID {
	return g.self
}

func (g *ActorBase) DelayInit(ctx context.Context) error {
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
