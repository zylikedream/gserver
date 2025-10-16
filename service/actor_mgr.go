package service

import (
	"gserver/core/gxyactor"

	"github.com/gogf/gf/v2/container/gmap"
)

type ActorMgr struct {
	actors *gmap.AnyAnyMap
}

func NewActorMgr() *ActorMgr {
	return &ActorMgr{
		actors: gmap.NewAnyAnyMap(true),
	}
}

func (s *ActorMgr) Add(id any, pid gxyactor.PID) {
	s.actors.Set(id, pid)
}

func (s *ActorMgr) Count() int {
	return s.actors.Size()
}
