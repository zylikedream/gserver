package gxyservice

import (
	"context"
	"gserver/core/gxyapp"
	"gserver/core/gxylog"
	"gserver/core/gxyregistery"
	"gserver/core/gxyutil"
)

type serviceApp struct {
	gxyapp.App
	Services         []IService
	serviceInfo      []*gxyregistery.ServiceInfo
	registry         gxyregistery.IRegistery
	nodeInstanceName string
}

var svrApp *serviceApp

func ServiceApp() *serviceApp {
	return svrApp
}

func NewServiceApp(nodeInstanceName string) *serviceApp {
	svrApp = &serviceApp{
		nodeInstanceName: nodeInstanceName,
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
			s.nodeInstanceName,
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
	gxylog.Info(ctx, "unregister services", gxylog.Num("svcnum", len(s.serviceInfo)))
	for _, svcInfo := range s.serviceInfo {
		if err := s.registry.UnRegister(ctx, svcInfo); err != nil {
			gxylog.Error(ctx, "unregister service failed", gxylog.Err(err))
			continue
		}
	}
	return nil
}

func (s *serviceApp) GetServiceInfo(ctx context.Context, name string, key string, selector gxyregistery.ServiceSelector) *gxyregistery.ServiceInfo {
	services, err := s.registry.GetHashServices(ctx, name)
	if err != nil {
		gxylog.Error(ctx, "get service failed", gxylog.Str("name", name), gxylog.Str("key", key), gxylog.Err(err))
		return nil
	}

	gxylog.Debug(ctx, "get service success", gxylog.Str("name", name), gxylog.Str("key", key),
		gxylog.Str("services", gxyutil.FormatObject(services)))

	return selector.Select(ctx, name, key, services)
}

// GetAddressByNodeName 通过 nodeInstanceName 查找节点地址（从 Consul Watcher 本地缓存）
func (s *serviceApp) GetAddressByNodeName(ctx context.Context, name string, nodeInstanceName string) string {
	services, err := s.registry.GetHashServices(ctx, name)
	if err != nil {
		gxylog.Error(ctx, "get services for node lookup failed", gxylog.Err(err))
		return ""
	}
	for _, svc := range services.ServiceInfos {
		if svc.NodeName == nodeInstanceName {
			return svc.NodeHost
		}
	}
	return ""
}
