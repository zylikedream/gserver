package gxyactor

import (
	"context"

	"ergo.services/ergo/gen"
)

// 类型别名 - 抽象层，隐藏具体实现但保持兼容性
type (
	PID  = gen.PID  // 进程ID
	Ref  = gen.Ref  // 引用（用于同步调用）
	Atom = gen.Atom // 原子（节点名称等）
)

// Message 消息接口
type Message interface {
	GetType() string
}

// BaseMessage 基础消息
type BaseMessage struct {
	Type string
}

func (m BaseMessage) GetType() string {
	return m.Type
}

// ActorContext Actor上下文
type ActorContext struct {
	Context context.Context
	Self    PID
	From    PID
}

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
