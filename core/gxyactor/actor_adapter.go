package gxyactor

import (
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

// ActorBase 将我们的ActorBehavior适配到ergo的act.ActorBehavior
type ActorBase struct {
	act.Actor
	behavior ActorBehavior
}

// Init 初始化Actor
func (a *ActorBase) Init(args ...any) error {
	return a.behavior.OnInit(args...)
}

// ====适配ergo的act.ActorBehavior ==
// HandleMessage 处理异步消息
func (a *ActorBase) HandleMessage(from gen.PID, message any) error {
	return a.behavior.OnHandleMessage(from, message)
}

// HandleCall 处理同步调用
func (a *ActorBase) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	return a.behavior.OnHandleCall(PID(from), Ref(ref), request)
}

// Terminate 终止处理
func (a *ActorBase) Terminate(reason error) {
	a.behavior.OnTerminate(reason)
}

// ====适配ergo的act.ActorBehavior end ==

// ActorBehavior的默认实现
func (a *ActorBase) OnInit(args ...any) error {
	// todo 打印log
	return nil
}

func (a *ActorBase) OnHandleMessage(from gen.PID, message any) error {
	// todo 打印log
	return nil
}

// OnHandleCall 处理同步调用
func (a *ActorBase) OnHandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	// todo 打印log
	return nil, nil
}

func (a *ActorBase) OnTerminate(reason error) {
	// todo 打印log
}

// NewActorAdapter 创建Actor适配器
func NewActorAdapter(behavior ActorBehavior) *ActorBase {
	return &ActorBase{
		behavior: behavior,
	}
}
