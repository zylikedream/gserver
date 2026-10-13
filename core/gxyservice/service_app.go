// service_app.go 实现模块树里的 "service" 模块:把本节点承载的服务注册进服务发现,
// 并按节点状态(见 gxynodeenv)持续刷新,使其他节点能寻址到这里。
package gxyservice

import (
	"context"

	"gserver/core/gxyapp"
	"gserver/core/gxylog"
	"gserver/core/gxynodeenv"
	"gserver/core/gxyregistery"
	"gserver/core/gxyregistery/types"
	"gserver/core/gxyutil"
	"time"
)

// serviceApp 服务模块:持有已加载的服务、注册表、节点环境与刷新协程的取消函数。
type serviceApp struct {
	gxyapp.App
	Services         []IService
	serviceInfo      []*types.ServiceInfo
	registry         gxyregistery.IRegistery
	nodeInstanceName string
	nodeEnv          gxynodeenv.NodeEnv
	refreshCancel    context.CancelFunc
}

var svrApp *serviceApp

// ServiceApp 返回全局服务模块实例。
func ServiceApp() *serviceApp {
	return svrApp
}

// NewServiceApp 构造服务注册模块。registry 为 nil 时由 OnModInit 按配置装配
// 真实注册表;测试在组装处传入内存实现(见 invariants.md「测试替身规则」)。
func NewServiceApp(nodeInstanceName string, registry gxyregistery.IRegistery) *serviceApp {
	svrApp = &serviceApp{
		nodeInstanceName: nodeInstanceName,
		registry:         registry,
	}
	return svrApp
}

// GetServices 返回本节点已加载的服务。
func (s *serviceApp) GetServices() []IService {
	return s.Services
}

// LoadService 加载服务:加入待注册列表,并挂到模块树(由其负责生命周期)。
func (s *serviceApp) LoadService(ctx context.Context, service IService) {
	s.Services = append(s.Services, service)
	if err := s.AddModule(ctx, service); err != nil {
		gxylog.Error(ctx, "load service module failed", gxylog.Str("service", service.ServiceName()), gxylog.Err(err))
	}
}

// OnModInit 装配注册表与节点环境探测;registry 已由外部注入时不再覆盖。
func (s *serviceApp) OnModInit(ctx context.Context) error {
	if s.registry == nil {
		registry, err := gxyregistery.NewRegistery()
		if err != nil {
			return err
		}
		s.registry = registry
	}
	s.nodeEnv = gxynodeenv.NewAutoNodeEnv()
	return nil
}

// OnModStartAfter 在所有模块启动完成后注册服务,并开启节点状态刷新。
// 放在这个阶段是因为注册要带上服务自身的地址,而地址只有启动后才确定。
func (s *serviceApp) OnModStartAfter(ctx context.Context) error {
	if err := s.registerServices(ctx); err != nil {
		return err
	}
	s.startServiceStateRefresh()
	return nil
}

// registerServices 逐个注册服务;节点状态整轮只读取一次。
func (s *serviceApp) registerServices(ctx context.Context) error {
	// 节点状态每轮只取一次:它与服务数量无关
	state, hasState := s.nodeState(ctx)
	for _, service := range s.Services {
		svcInfo := types.NewServiceInfo(
			service.ServiceName(),
			s.nodeInstanceName,
			service.Host(),
			service.Version(), service.Weight())
		if hasState {
			svcInfo.State = state
		}
		if err := s.registry.Register(ctx, svcInfo); err != nil {
			return err
		}
		s.serviceInfo = append(s.serviceInfo, svcInfo)

	}
	return nil
}

// startServiceStateRefresh 启动周期刷新协程;无节点环境或无注册服务时不必刷新。
func (s *serviceApp) startServiceStateRefresh() {
	if s.nodeEnv == nil || len(s.serviceInfo) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.refreshCancel = cancel
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.refreshRegisteredServices(ctx)
			}
		}
	}()
}

// refreshRegisteredServices 刷新一轮节点状态;状态未变化的服务不重复注册。
func (s *serviceApp) refreshRegisteredServices(ctx context.Context) {
	state, ok := s.nodeState(ctx)
	if !ok {
		return
	}
	for _, svcInfo := range s.serviceInfo {
		if svcInfo.State == state {
			continue
		}
		svcInfo.State = state
		if err := s.registry.Register(ctx, svcInfo); err != nil {
			gxylog.Error(ctx, "refresh service state failed", gxylog.Str("service", svcInfo.Name), gxylog.Str("node", svcInfo.NodeName), gxylog.Err(err))
			continue
		}
		gxylog.Info(ctx, "refresh service state", gxylog.Str("service", svcInfo.Name), gxylog.Str("node", svcInfo.NodeName), gxylog.Str("state", string(svcInfo.State)))
	}
}

// nodeState 读取节点状态;第二个返回值表示读取是否成功,失败时调用方应保留原状态。
func (s *serviceApp) nodeState(ctx context.Context) (types.ServiceState, bool) {
	if s.nodeEnv == nil {
		return types.ServiceStateDraining, false
	}
	state, err := s.nodeEnv.State(ctx)
	if err != nil {
		gxylog.Warn(ctx, "load node state failed", gxylog.Err(err))
		return "", false
	}
	return state, true
}

// OnModStop 停止刷新协程,并注销本节点注册过的全部服务。
func (s *serviceApp) OnModStop(ctx context.Context) error {
	if s.refreshCancel != nil {
		s.refreshCancel()
		s.refreshCancel = nil
	}
	gxylog.Info(ctx, "unregister services", gxylog.Num("svcnum", len(s.serviceInfo)))
	for _, svcInfo := range s.serviceInfo {
		if err := s.registry.UnRegister(ctx, svcInfo); err != nil {
			gxylog.Error(ctx, "unregister service failed", gxylog.Err(err))
			continue
		}
	}
	return nil
}

// GetServiceInfo 按 key 在 name 类服务里选一个目标地址;选择算法由 selector 决定。
func (s *serviceApp) GetServiceInfo(ctx context.Context, name string, key string, selector gxyregistery.ServiceSelector) *types.ServiceInfo {
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
