package logic

import (
	"gserver/service"
)

var roleMgr = service.NewActorMgr()

func RoleMgr() *service.ActorMgr {
	return roleMgr
}
