package gxyactor

import (
	"context"
	"time"

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

// ActorSystem Actor系统接口
type IActorSystem interface {
	// Spawn 创建Actor
	SpawnRegister(name string, actor ActorBehavior, options gen.ProcessOptions, args ...any) (PID, error)

	// Spawn 创建Actor
	Spawn(actor ActorBehavior, options gen.ProcessOptions, args ...any) (PID, error)

	// Send 异步发送消息
	Send(pid PID, message any) error

	// Kill 终止Actor
	StopActor(pid PID) error

	// NodeName 获取节点名称
	NodeName() string
}

// MessageType 消息类型常量
type MessageType string

const (
	TypeActorCreated MessageType = "actor.created"
	TypeActorKilled  MessageType = "actor.killed"
	TypeCallTimeout  MessageType = "call.timeout"
)

// SystemMessage 系统消息
type SystemMessage struct {
	BaseMessage
	Data any
}

// NewSystemMessage 创建系统消息
func NewSystemMessage(msgType MessageType, data any) SystemMessage {
	return SystemMessage{
		BaseMessage: BaseMessage{Type: string(msgType)},
		Data:        data,
	}
}

// CallOptions 同步调用选项
type CallOptions struct {
	Timeout time.Duration
}

// DefaultCallOptions 默认调用选项
var DefaultCallOptions = CallOptions{
	Timeout: 5 * time.Second,
}
