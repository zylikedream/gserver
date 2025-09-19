package logic

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxymodule"
	"gserver/core/gxynet/endpoint"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/os/glog"
)

// sessionSupervisor 会话管理器 - 直接继承gen.Supervisor，本身即是Supervisor
type sessionManager struct {
	gxymodule.Module
	actor gen.PID
}

type sessionSupervisor struct {
	act.Supervisor
}

var sessionMgr *sessionSupervisor

// NewSessionManager 创建会话管理器
func NewSessionManager() *sessionManager {
	pid, err := gxyactor.ActorSystem().SpawnRegister("session_manager", func() gen.ProcessBehavior {
		return &sessionSupervisor{}
	}, gen.ProcessOptions{})
	if err != nil {
		glog.Fatalf(context.Background(), "create session manager failed, err: %v", err)
		return nil
	}
	sessmgr := &sessionManager{
		actor: pid,
	}
	return sessmgr
}

// Init Supervisor初始化
func (ss *sessionSupervisor) Init(args ...any) (act.SupervisorSpec, error) {
	spec := act.SupervisorSpec{
		Type: act.SupervisorTypeSimpleOneForOne,
	}
	spec.Children = []act.SupervisorChildSpec{
		{
			Name: "session",
			Factory: func() gen.ProcessBehavior {
				return &Session{}
			},
		},
	}
	return spec, nil
}

func (ss *sessionSupervisor) HandleMessage(from gen.PID, msg any) error {
	return nil
}

func (ss *sessionSupervisor) HandleCall(ctx context.Context, msg any) error {
	return nil
}

func (ss *sessionSupervisor) HandleM() string {
	return "session_supervisor"
}

// CreateSession 创建Session Actor（通过Supervisor自动管理）
func (ss *sessionSupervisor) CreateSession(ep endpoint.Endpoint) error {
	connID := ep.Conn().RemoteAddr().String()
	// 使用Supervisor的StartChild创建并监控Session
	err := ss.StartChild(gen.Atom(connID), connID, ep)
	return err
}

func (ss *sessionSupervisor) StopSession(ep endpoint.Endpoint, reason string) error {
	connID := ep.Conn().RemoteAddr().String()
	// 使用Supervisor的StopChild停止并删除Session
	return ss.Send(gen.Atom(connID), NewSystemMessage(SysMsgStop, reason))
}

// CreateSession 创建Session Actor（通过Supervisor自动管理）
func (sm *sessionManager) CreateSession(ep endpoint.Endpoint) error {
	connID := ep.Conn().RemoteAddr().String()
	// 使用Supervisor的StartChild创建并监控Session
	gxyactor.ActorSystem().Send(sm.actor)
	return err
}

func (sm *sessionManager) StopSession(ep endpoint.Endpoint, reason string) error {
	connID := ep.Conn().RemoteAddr().String()
	// 使用Supervisor的StopChild停止并删除Session
	return ss.Send(gen.Atom(connID), NewSystemMessage(SysMsgStop, reason))
}
