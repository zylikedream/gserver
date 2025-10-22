package gxyactor

import "gserver/core/gxyservice"

type ActorService struct {
	gxyservice.PublicService
}

func (s *ActorService) Host() string {
	return ActorSystem().Address()
}
