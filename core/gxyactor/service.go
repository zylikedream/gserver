package gxyactor

import (
	"gserver/core/gxymodule"
)

const (
	DEFAULT_WEIGHT  = 1
	DEFAULT_VERSION = "v1.0.0"
)

type IService interface {
	gxymodule.IModule
	Name() string
	Weight() int
	Public() bool
	Version() string
}

type Service struct {
	gxymodule.ModuleBase
}

func (s *Service) Name() string {
	return s.GetModName()
}

func (s *Service) Public() bool {
	return true
}

func (s *Service) Weight() int {
	return DEFAULT_WEIGHT
}

func (s *Service) Version() string {
	return DEFAULT_VERSION
}

type InnerService struct {
	gxymodule.ModuleBase
}

func (s *InnerService) Name() string {
	return s.GetModName()
}

func (s *InnerService) Public() bool {
	return false
}

func (s *InnerService) Weight() int {
	return DEFAULT_WEIGHT
}

func (s *InnerService) Version() string {
	return DEFAULT_VERSION
}
