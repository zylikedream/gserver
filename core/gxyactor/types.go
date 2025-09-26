package gxyactor

import (
	"github.com/tochemey/goakt/v3/actor"
)

// 类型别名 - 抽象层，隐藏具体实现但保持兼容性
type (
	PID = *actor.PID // 进程ID
)
