package logic

import (
	"gserver/core/gxyactor"
	"ergo.services/ergo/gen"
)

// SessionManager 会话管理器 - 直接继承gen.Supervisor，本身即是Supervisor
type SessionManager struct {
	gen.Supervisor
	actorSystem *gxyactor.ActorSystem
}

// NewSessionManager 创建会话管理器
func NewSessionManager(actorSystem *gxyactor.ActorSystem) *SessionManager {
	return &SessionManager{
		actorSystem: actorSystem,
	}
}

// Init Supervisor初始化
func (sm *SessionManager) Init(args ...any) error {
	// Supervisor策略：简单的一对一重启策略
	strategy := gen.SupervisorStrategy{
		Type: gen.SupervisorStrategyOneForOne,
		Restart: gen.SupervisorRestart{
			Attempts: 3,
			Period:   10,
		},
	}

	return sm.Supervisor.Init(strategy)
}

// CreateSession 创建Session Actor（通过Supervisor自动管理）
func (sm *SessionManager) CreateSession(connID string, args ...any) (gxyactor.PID, error) {
	// 使用Supervisor的StartChild创建并监控Session
	pid, err := sm.StartChild(connID, gen.ProcessOptions{}, NewSession, args...)
	if err != nil {
		return gxyactor.PID{}, err
	}

	return gxyactor.PID(pid), nil
}