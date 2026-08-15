package gxyservice

import (
	"gserver/core/gxymodule"
)

const (
	DEFAULT_WEIGHT  = 1
	DEFAULT_VERSION = "v1.0.0"
)

type IService interface {
	gxymodule.IModule
	ServiceName() string
	Weight() int
	Version() string
	Host() string
}

type Service struct {
	gxymodule.ModuleBase
}

func (s *Service) ServiceName() string {
	return s.GetModName()
}

func (s *Service) Weight() int {
	return DEFAULT_WEIGHT
}

func (s *Service) Version() string {
	return DEFAULT_VERSION
}
