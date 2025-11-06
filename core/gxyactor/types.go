package gxyactor

import "gserver/core/gxyservice"

type ActorService struct {
	gxyservice.Service
}

func (s *ActorService) Host() string {
	return Address()
}
