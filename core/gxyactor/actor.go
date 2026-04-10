package gxyactor

import (
	"context"
	"gserver/core/gxylog"
	"gserver/core/gxytimer"
	"gserver/protocol/pb"
	"gserver/util"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/gutil"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
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

type ActorContext struct {
	actor.Context
	InitArgs []any
}

type GrainProducer func() IGrain
type IActor interface {
	Init(ctx context.Context) error
	DelayInit(ctx context.Context) error
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

func NewActorBase(ctx context.Context, actor IActor) *ActorBase {
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
		a.Send(a.self, &ActorInitMsg{})
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
			glog.Errorf(a.ctx, "timer active error, msg:%s, error:%+v", msg.Name, err)
		}
	case *pb.ActorCallMessage:
		rsp, err := a.handleCallMsg(a.ctx, msg)
		if err != nil {
			glog.Errorf(a.ctx, "handle rpc msg failed, msg:%s, error:%+v", msg.Msg.String(), err)
			a.Respond(&pb.ActorError{
				Reason: err.Error(),
			})
			return nil
		}
		if rsp != nil {
			a.Respond(rsp)
		}
	case *pb.ActorStop:
		a.Stop(gerror.New(msg.Reason))
	case *actor.Stopping:
		return nil
	case *actor.Stopped:
		a.timer.Stop(a.ctx)
		a.actor.Terminate(a.ctx, a.stopErr)
	default:
		glog.Errorf(a.ctx, "handle ")
	}
	return nil
}

func (a *ActorBase) handlePushMsg(ctx context.Context, msg *pb.PushMessage) error {
	meta := a.msgHandler.GetMethodMetaByName(msg.MsgName)
	if meta == nil {
		return gerror.Newf("method not found for msg %s", msg.MsgName)
	}
	arg := util.NewObject(meta.ArgType)
	if err := util.DecodeMsg([]byte(msg.MsgData), arg); err != nil {
		return gerror.Wrapf(err, "unmarshal push msg error, msg: %s", msg.MsgName)
	}
	// push 没有返回值
	_, err := a.CallHandlerMsg(ctx, arg)
	if err != nil {
		return err
	}
	return nil
}

func (a *ActorBase) handleCallMsg(ctx context.Context, msg *pb.ActorCallMessage) (proto.Message, error) {
	pbmsg, err := anypb.UnmarshalNew(msg.GetMsg(), proto.UnmarshalOptions{})
	if err != nil {
		return nil, gerror.Wrapf(err, "unmarshal call msg error(msg must be protobuf msg), msg: %s", msg.String())
	}
	return a.HandleProtobufMsg(ctx, pbmsg)
}

func (a *ActorBase) HandleProtobufMsg(ctx context.Context, msg proto.Message) (proto.Message, error) {
	tm := time.Now()
	glog.Debugf(ctx, "handle msg start, msg: %v", util.FormatObject(msg))
	result, err := a.CallHandlerMsg(ctx, msg)
	glog.Debugf(ctx, "handle msg end, msg: %s, result: %s, err %+v, cost: %vms",
		util.FormatObject(msg), util.FormatObject(result), err, time.Since(tm).Milliseconds())
	if err != nil {
		return nil, err
	}
	var rsp proto.Message
	if result != nil {
		rsp, _ = result.(proto.Message)
	}
	return rsp, nil
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

func (a *ActorBase) Init(ctx context.Context) error {
	return nil
}

func (a *ActorBase) DelayInit(ctx context.Context) error {
	return nil
}

func (a *ActorBase) Terminate(ctx context.Context, err error) {
}

// 回应CallSync方法
func (a *ActorBase) Respond(msg any) {
	if a.Actx.Sender() == nil {
		glog.Infof(a.ctx, "respond sender is nil, msg: %v", msg)
		return
	}
	a.Actx.Respond(msg)
}

// 发送请求里带了sender，所以接收方可以调用respond回应消息
func (a *ActorBase) CallSync(pid PID, msg proto.Message) {
	a.Actx.Request(pid, msg)
}

func (a *ActorBase) Call(pid PID, msg proto.Message, timeout time.Duration) (any, error) {
	return a.Actx.RequestFuture(pid, msg, timeout).Result()
}

func (a *ActorBase) Send(pid PID, msg proto.Message) {
	a.Actx.Send(pid, msg)
}

func (a *ActorBase) Sender() PID {
	return a.Actx.Sender()
}

func (a *ActorBase) SpawnNamed(props *actor.Props, name string) (PID, error) {
	return a.Actx.SpawnNamed(props, name)
}

func (a *ActorBase) Spawn(props *actor.Props) PID {
	return a.Actx.Spawn(props)
}

func (a *ActorBase) SetLogValue(key string, val any) *ActorBase {
	a.ctx = gxylog.WithValue(a.ctx, key, val)
	return a
}

func (a *ActorBase) AddMsgHandler(handler any, prefix ...string) {
	a.msgHandler.AddHandler(handler, prefix...)
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
	base.ActorBase = NewActorBase(ctx, grain)
	return base
}

func (g *GrainBase) GrainID() string {
	return g.grainID
}

func (g *GrainBase) Receive(ctx actor.Context) {
	switch ctx.Message().(type) {
	case *actor.Started:
		actorCtx := ctx.(*ActorContext)
		g.grainID = actorCtx.InitArgs[0].(string)
	}
	g.ActorBase.Receive(ctx)
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
