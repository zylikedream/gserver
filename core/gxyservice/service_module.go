package gxyservice

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxymodule"
	"gserver/core/gxyregistery"
)

type serviceModule struct {
	gxymodule.Module
	registry gxyregistery.IRegistery
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
}

func (s *serviceModule) OnInit(ctx context.Context) error {
	var err error
	if err = s.Module.OnInit(ctx); err != nil {
		return err
	}
	s.registry, err = gxyregistery.NewRegistery(gxyregistery.REGISTERY_TYPE_ETCD, "../config/service.toml")
	if err != nil {
		return err
	}
	return nil
}

func (s *serviceModule) OnStart(ctx context.Context) error {
	system := gxyactor.ActorSystem().GetActorSystem()
	for _, svc := range s.Services {
		if err := svc.OnStart(ctx); err != nil {
			return err
		}
		if !svc.IsRemote() {
			continue
		}
		system.Register(ctx, svc.Worker())

		if err := s.registry.Register(ctx, svc.Name(), system.Host()); err != nil {
			return err
		}
	}
	return nil
}

func (s *serviceModule) GetServiceNode(service string, selector gxyregistery.ServiceSelector) gxyregistery.ServiceNode {
	nodes, err := s.registry.Search(context.Background(), service)
	if err != nil {
		return gxyregistery.ServiceNode{}
	}

	return selector.Select(service, nodes)
}
