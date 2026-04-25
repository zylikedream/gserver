package gxyservice

import (
	"context"
	"gserver/core/gxyapp"
	"gserver/core/gxyregistery"
	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/os/glog"
)

type serviceApp struct {
	gxyapp.App
	Services    []IService
	serviceInfo []*gxyregistery.ServiceInfo
	registry    gxyregistery.IRegistery
	nodeName    string
}

var svrApp *serviceApp

func ServiceApp() *serviceApp {
	return svrApp
}

func NewServiceApp(nodeName string) *serviceApp {
	svrApp = &serviceApp{
		nodeName: nodeName,
	}
	return svrApp
}

func (s *serviceApp) GetServices() []IService {
	return s.Services
}

func (s *serviceApp) LoadService(ctx context.Context, service IService) {
	s.Services = append(s.Services, service)
	s.AddModule(ctx, service)
}

func (s *serviceApp) OnModInit(ctx context.Context) error {
	registry, err := gxyregistery.NewRegistery()
	if err != nil {
		return err
	}
	s.registry = registry
	return nil
}

// 确保所有启动都启动好了以后再注册
func (s *serviceApp) OnModStartAfter(ctx context.Context) error {
	if err := s.registerSevices(); err != nil {
		return err
	}
	return nil
}

func (s *serviceApp) registerSevices() error {
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

func (s *serviceApp) OnModStop(ctx context.Context) error {
	glog.Infof(ctx, "unregister %d services", len(s.serviceInfo))
	for _, svcInfo := range s.serviceInfo {
		if err := s.registry.UnRegister(ctx, svcInfo); err != nil {
			glog.Errorf(ctx, "unregister service failed:%+v", err)
			continue
		}
	}
	return nil
}

func (s *serviceApp) GetServiceInfo(ctx context.Context, name string, key string, selector gxyregistery.ServiceSelector) *gxyregistery.ServiceInfo {
	services, err := s.registry.GetHashServices(ctx, name)
	if err != nil {
		glog.Errorf(ctx, "get service(%s:%s) failed:%+v", name, key, err)
		return nil
	}
	glog.Debugf(ctx, "get service(%s:%s) success, services: %s", name, key, gxyutil.FormatObject(services))
	return selector.Select(ctx, name, key, services)
}
