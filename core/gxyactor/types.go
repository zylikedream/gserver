package gxyactor

import (
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

// 类型别名 - 抽象层，隐藏具体实现但保持兼容性
type (
	PID  = gen.PID  // 进程ID
	Ref  = gen.Ref  // 引用（用于同步调用）
	Atom = gen.Atom // 原子（节点名称等）
)

// ActorBehavior Actor行为接口（完全抽象，依赖ergo基础类型但隐藏实现）
type ActorBehavior interface {
	// Init 初始化
	OnInit(args ...any) error

	// HandleMessage 处理异步消息
	OnHandleMessage(from PID, message any) error

	// HandleCall 处理同步调用
	OnHandleCall(from PID, ref Ref, request any) (any, error)

	// Terminate 终止处理
	OnTerminate(reason error)
}

type ActorBaseMessageBehavior interface {
	OnHandleMessage(from gen.PID, msg ActorMessage) error
}
type ActorBaseCallBehavior interface {
	OnHandleCall(from gen.PID, msg ActorMessage) (any, error)
}

type ActorBase struct {
	act.Actor
	msgBehavior  ActorBaseMessageBehavior
	callBehavior ActorBaseCallBehavior
}

func (a *ActorBase) ProcessInit(process gen.Process, args ...any) (err error) {
	a.msgBehavior, _ = process.Behavior().(ActorBaseMessageBehavior)
	a.callBehavior, _ = process.Behavior().(ActorBaseCallBehavior)
	return a.Actor.ProcessInit(process, args...)
}

func (a *ActorBase) HandleMessage(from gen.PID, msg any) error {
	return a.msgBehavior.OnHandleMessage(from, msg.(ActorMessage))
}

func (a *ActorBase) HandleCall(from gen.PID, ref gen.Ref, msg any) (any, error) {
	return a.callBehavior.OnHandleCall(from, msg.(ActorMessage))
}

func (a *ActorBase) OnHandleMessage(from gen.PID, msg ActorMessage) error {
	return nil
}

func (a *ActorBase) OnHandleCall(from gen.PID, msg ActorMessage) (any, error) {
	return nil, nil
}
