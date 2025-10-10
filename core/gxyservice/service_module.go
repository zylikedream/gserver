package gxyservice

import (
	"context"
	"gserver/core/gxymodule"
)

type serviceModule struct {
	gxymodule.Module
	Services []IService
}

var svrMod *serviceModule

func ServiceModule() *serviceModule {
	return svrMod
}

func NewService() *serviceModule {
	svrMod = &serviceModule{}
	return svrMod
}

func (s *serviceModule) GetServices() []IService {
	return s.Services
}

func (s *serviceModule) LoadService(service IService) {
	s.Services = append(s.Services, service)
	s.Module.AddModule(context.Background(), service)
}

func (s *serviceModule) OnInit(ctx context.Context) error {
	if err := s.Module.OnInit(ctx); err != nil {
		return err
	}
	return nil
}

func (s *serviceModule) OnStart(ctx context.Context) error {
	if err := s.Module.OnStart(ctx); err != nil {
		return err
	}
	return nil
}
