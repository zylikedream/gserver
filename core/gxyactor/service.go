package gxyactor

import (
	"gserver/core/gxymodule"
)

type IService interface {
	gxymodule.IModule
	Name() string
	Weight() int
	Public() bool
}

type Service struct {
	gxymodule.Module
}

func (s *Service) Name() string {
	return s.GetName()
}

func (s *Service) Public() bool {
	return true
}

type InnerService struct {
	gxymodule.Module
}

func (s *InnerService) Name() string {
	return s.GetName()
}

func (s *InnerService) Public() bool {
	return false
}

func (s *InnerService) Weight() int {
	return 0
}
