package logic

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxynet/endpoint"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/os/glog"
)

const (
	SESSIONMGR_MSG_CREATE_SESSION = "create_session"
	SESSIONMGR_MSG_STOP_SESSION   = "stop_session"
)

type sessionmgrMsgStop struct {
	Ep     endpoint.Endpoint
	Reason error
}

// sessionSupervisor 会话管理器 - 直接继承gen.Supervisor，本身即是Supervisor
type sessionManager struct {
	sup gen.PID
}

type sessionSupervisor struct {
	act.Supervisor
}

var sessionMgr *sessionManager

func SessionManager() *sessionManager {
	return sessionMgr
}

// NewSessionManager 创建会话管理器
func NewSessionManager() *sessionManager {
	pid, err := gxyactor.ActorSystem().SpawnRegister("session_manager", func() gen.ProcessBehavior {
		return &sessionSupervisor{}
	}, gen.ProcessOptions{})
	if err != nil {
		glog.Fatalf(context.Background(), "create session manager failed, err: %v", err)
		return nil
	}
	sessionMgr = &sessionManager{
		sup: pid,
	}
	return sessionMgr
}

// Init Supervisor初始化
func (ss *sessionSupervisor) Init(args ...any) (act.SupervisorSpec, error) {
	spec := act.SupervisorSpec{
		Type:              act.SupervisorTypeSimpleOneForOne,
		EnableHandleChild: true,
	}
	spec.Children = []act.SupervisorChildSpec{
		{
			Name: "session",
			Factory: func() gen.ProcessBehavior {
				return &Session{}
			},
		},
	}
	spec.Restart = act.SupervisorRestart{
		Strategy:  act.SupervisorStrategyTemporary,
		Intensity: 2,
		Period:    5,
	}
	return spec, nil
}

func (ss *sessionSupervisor) OnHandleMessage(from gen.PID, msg gxyactor.ActorMessage) error {
	var err error
	switch msg.Name {
	case SESSIONMGR_MSG_CREATE_SESSION:
		err = ss.createSession(msg.Data.(endpoint.Endpoint))
	case SESSIONMGR_MSG_STOP_SESSION:
		stopMsg := msg.Data.(sessionmgrMsgStop)
		err = ss.stopSession(stopMsg.Ep, stopMsg.Reason)
	default:
		glog.Errorf(context.Background(), "unknown message type: %s", msg.Name)
	}

	if err != nil {
		glog.Errorf(context.Background(), "create session failed, err: %v", err)
	}
	return err
}

// CreateSession 创建Session Actor（通过Supervisor自动管理）
func (ss *sessionSupervisor) createSession(ep endpoint.Endpoint) error {
	return ss.StartChild("session", ep)
}

func (ss *sessionSupervisor) stopSession(ep endpoint.Endpoint, reason error) error {
	sessPid, ok := ep.GetData().(gen.PID)
	if !ok {
		glog.Errorf(context.Background(), "Failed to get session from endpoint data")
		return nil
	}
	return ss.SendExit(sessPid, reason)
}

// CreateSession 创建Session Actor（通过Supervisor自动管理）
func (sm *sessionManager) CreateSession(ep endpoint.Endpoint) error {
	// 使用Supervisor的StartChild创建并监控Session
	err := gxyactor.ActorSystem().Send(sm.sup, gxyactor.NewActorMessage(SESSIONMGR_MSG_CREATE_SESSION, ep))
	return err
}

func (sm *sessionManager) StopSession(ep endpoint.Endpoint, reason error) error {
	return gxyactor.ActorSystem().Send(sm.sup, gxyactor.NewActorMessage(SESSIONMGR_MSG_STOP_SESSION,
		sessionmgrMsgStop{
			Ep:     ep,
			Reason: reason,
		}))
}
