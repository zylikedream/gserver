package gxyactor

import (
	"context"
	"gserver/core/gxymodule"
	"gserver/core/gxyregistery"
)

type serviceMgr struct {
	gxymodule.Module
	Services []IService
	registry gxyregistery.IRegistery
}

var svrMgr *serviceMgr

func ServiceManager() *serviceMgr {
	return svrMgr
}

func NewServiceManager() *serviceMgr {
	svrMgr = &serviceMgr{}
	return svrMgr
}

func (s *serviceMgr) GetServices() []IService {
	return s.Services
}

func (s *serviceMgr) LoadService(service IService) {
	s.Services = append(s.Services, service)
	s.Module.AddModule(context.Background(), service)
}

func (s *serviceMgr) OnInit(ctx context.Context) error {
	if err := s.Module.OnInit(ctx); err != nil {
		return err
	}
	registry, err := gxyregistery.NewRegistery(gxyregistery.REGISTERY_TYPE_ETCD, "node/config/service.toml")
	if err != nil {
		return err
	}
	s.registry = registry
	return nil
}

func (s *serviceMgr) OnStart(ctx context.Context) error {
	if err := s.Module.OnStart(ctx); err != nil {
		return err
	}
	for _, service := range s.Services {
		if !service.Public() {
			continue
		}
		if err := s.registry.Register(context.Background(), service.Name(), ActorSystem().Address()); err != nil {
			return err
		}
	}
	return nil
}

func (s *serviceMgr) OnStop(ctx context.Context) error {
	if err := s.Module.OnStop(ctx); err != nil {
		return err
	}
	for _, service := range s.Services {
		if !service.Public() {
			continue
		}
		if err := s.registry.UnRegister(context.Background(), service.Name(), ActorSystem().Address()); err != nil {
			return err
		}
	}
	return nil
}

func (s *serviceMgr) GetServiceNode(name string, selector gxyregistery.ServiceSelector) gxyregistery.ServiceNode {
	nodes, err := s.registry.Search(context.Background(), name)
	if err != nil {
		return gxyregistery.ServiceNode{}
	}

	return selector.Select(name, nodes)
}
