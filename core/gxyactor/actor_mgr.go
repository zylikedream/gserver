package gxyactor

import (
	"github.com/gogf/gf/v2/container/gmap"
)

type ActorMgr struct {
	actors *gmap.AnyAnyMap
	name   string
}

func NewActorMgr(name string) *ActorMgr {
	return &ActorMgr{
		actors: gmap.NewAnyAnyMap(true),
		name:   name,
	}
}

func (s *ActorMgr) Add(id any, pid PID) {
	s.actors.Set(id, pid)
}

func (s *ActorMgr) Remove(id any) {
	s.actors.Remove(id)
}

func (s *ActorMgr) Count() int {
	return s.actors.Size()
}

func (s *ActorMgr) Get(id any) PID {
	pid, _ := s.actors.Get(id).(PID)
	return pid
}

func (s *ActorMgr) All() []PID {
	pids := make([]PID, 0, s.actors.Size())
	s.actors.Iterator(func(_id any, v any) bool {
		pids = append(pids, v.(PID))
		return true
	})
	return pids
}
