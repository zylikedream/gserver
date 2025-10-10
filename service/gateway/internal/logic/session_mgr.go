package logic

import (
	"gserver/core/gxyactor"

	"github.com/asynkron/protoactor-go/actor"
)

const SessionMgrName = "session_mgr"

type SessionMgr struct {
	sessions map[int64]gxyactor.PID
}

type AddSessionMsg struct {
	RoleID int64
	PID    gxyactor.PID
}

func NewSessionMgr() actor.Actor {
	return &SessionMgr{
		sessions: make(map[int64]gxyactor.PID),
	}
}

func (s *SessionMgr) addSession(roleID int64, pid gxyactor.PID) {
	s.sessions[roleID] = pid
}

func (s *SessionMgr) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *AddSessionMsg:
		s.addSession(msg.RoleID, msg.PID)
	}
}

func AddSession(roleID int64, pid gxyactor.PID) {
	msg := &AddSessionMsg{
		RoleID: roleID,
		PID:    pid,
	}
	mgr := gxyactor.ActorSystem().GetActorSystem().NewLocalPID(SessionMgrName)
	gxyactor.ActorSystem().Send(mgr, msg)
}
