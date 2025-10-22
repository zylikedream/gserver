package logic

import (
	"gserver/core/gxyactor"
)

var sessMgr *gxyactor.ActorMgr

func NewSessionMgr() *gxyactor.ActorMgr {
	sessMgr = gxyactor.NewActorMgr("session_mgr")
	return sessMgr
}

func SessionMgr() *gxyactor.ActorMgr {
	return sessMgr
}
