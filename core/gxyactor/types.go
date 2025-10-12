package gxyactor

import (
	"gserver/core/gxytimer"

	"github.com/asynkron/protoactor-go/actor"
)

// 类型别名 - 抽象层，隐藏具体实现但保持兼容性
type (
	PID = *actor.PID // 进程ID
)

type ActorTimerMsg gxytimer.TimerActiveInfo
