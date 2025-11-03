package gxyservice

import (
	"context"
	"gserver/core/gxymodule"
	"gserver/core/gxyregistery"
	"gserver/util"

	"github.com/gogf/gf/v2/os/glog"
)

type serviceMgr struct {
	gxymodule.ModuleBase
	Services    []IService
	serviceInfo []*gxyregistery.ServiceInfo
	registry    gxyregistery.IRegistery
	nodeName    string
}

var svrMgr *serviceMgr

func ServiceManager() *serviceMgr {
	return svrMgr
}

func NewServiceManager(nodeName string) *serviceMgr {
	svrMgr = &serviceMgr{
		nodeName: nodeName,
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

func (s *serviceMgr) OnModInit(ctx context.Context) error {
	registry, err := gxyregistery.NewRegistery(gxyregistery.REGISTERY_TYPE_CONSUL, "node/config/service.toml")
	if err != nil {
		return err
	}
	s.registry = registry
	return nil
}

// 确保所有启动都启动好了以后再注册
func (s *serviceMgr) OnModStartAfter(ctx context.Context) error {
	if err := s.registerSevices(); err != nil {
		return err
	}
	return nil
}

func (s *serviceMgr) registerSevices() error {
	for _, service := range s.Services {
		svcInfo := gxyregistery.NewServiceInfo(
			service.ServiceName(),
			s.nodeName,
			service.Host(),
			service.Version(), service.Weight())
		if err := s.registry.Register(context.Background(), svcInfo); err != nil {
			return err
		}
		s.serviceInfo = append(s.serviceInfo, svcInfo)

	}
	return nil
}

func (s *serviceMgr) OnModStop(ctx context.Context) error {
	glog.Infof(ctx, "unregister %d services", len(s.serviceInfo))
	for _, svcInfo := range s.serviceInfo {
		if err := s.registry.UnRegister(ctx, svcInfo); err != nil {
			glog.Errorf(ctx, "unregister service failed:%+v", err)
			continue
		}
	}
	return nil
}

func (s *serviceMgr) GetServiceInfo(ctx context.Context, name string, key string, selector gxyregistery.ServiceSelector) *gxyregistery.ServiceInfo {
	services, err := s.registry.GetHashServices(ctx, name)
	if err != nil {
		glog.Errorf(ctx, "get service(%s:%s) failed:%+v", name, key, err)
		return nil
	}
	glog.Debugf(ctx, "get service(%s:%s) success, services: %s", name, key, util.FormatObject(services))
	return selector.Select(ctx, name, key, services)
}
