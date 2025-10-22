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
	Name() string
	Weight() int
	Public() bool
	Version() string
	Host() string
}

type PublicService struct {
	baseService
}

func (s *PublicService) Public() bool {
	return true
}

type InnerService struct {
	baseService
}

type baseService struct {
	gxymodule.ModuleBase
}

func (s *baseService) Name() string {
	return s.GetModName()
}

func (s *baseService) Public() bool {
	return false
}

func (s *baseService) Weight() int {
	return DEFAULT_WEIGHT
}

func (s *baseService) Version() string {
	return DEFAULT_VERSION
}

func (s *baseService) Host() string {
	return ""
}
