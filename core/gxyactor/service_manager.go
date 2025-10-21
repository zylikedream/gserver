package gxyactor

import (
	"context"
	"gserver/core/gxymodule"
	"gserver/core/gxyregistery"
	"gserver/core/gxytimer"

	"github.com/gogf/gf/v2/os/glog"
)

type serviceMgr struct {
	gxymodule.ModuleBase
	Services    []IService
	serviceInfo []*gxyregistery.ServiceInfo
	registry    gxyregistery.IRegistery
	timer       *gxytimer.GxyTimer
}

var svrMgr *serviceMgr

func ServiceManager() *serviceMgr {
	return svrMgr
}

func NewServiceManager() *serviceMgr {
	svrMgr = &serviceMgr{
		timer: gxytimer.NewTimer(),
	}
	return svrMgr
}

func (s *serviceMgr) GetServices() []IService {
	return s.Services
}

func (s *serviceMgr) LoadService(service IService) {
	s.Services = append(s.Services, service)
	s.ModuleBase.AddModule(context.Background(), service)
}

func (s *serviceMgr) OnInit(ctx context.Context) error {
	registry, err := gxyregistery.NewRegistery(gxyregistery.REGISTERY_TYPE_CONSUL, "node/config/service.toml")
	if err != nil {
		return err
	}
	s.registry = registry
	return nil
}

func (s *serviceMgr) OnStart(ctx context.Context) error {
	if err := s.registerSevices(); err != nil {
		return err
	}
	return nil
}

func (s *serviceMgr) registerSevices() error {
	for _, service := range s.Services {
		if !service.Public() {
			continue
		}
		svcInfo := gxyregistery.NewServiceInfo(
			service.Name(),
			ActorSystem().NodeName(),
			ActorSystem().Address(),
			service.Version(), service.Weight())
		if err := s.registry.Register(context.Background(), svcInfo); err != nil {
			return err
		}
		s.serviceInfo = append(s.serviceInfo, svcInfo)

	}
	return nil
}

func (s *serviceMgr) OnStop(ctx context.Context) error {
	glog.Infof(ctx, "unregister %d services", len(s.serviceInfo))
	for _, svcInfo := range s.serviceInfo {
		if err := s.registry.UnRegister(ctx, svcInfo); err != nil {
			glog.Errorf(ctx, "unregister service failed:%+v", err)
			continue
		}
	}
	s.timer.CancelAll()
	return nil
}

func (s *serviceMgr) GetServiceNode(name string, key string, selector gxyregistery.ServiceSelector) *gxyregistery.ServiceInfo {
	services, err := s.registry.Search(context.Background(), name)
	if err != nil {
		return nil
	}

	return selector.Select(name, key, services)
}
