package logic

import (
	"gserver/service"
)

var sessMgr = service.NewActorMgr()

func SessionMgr() *service.ActorMgr {
	return sessMgr
}
