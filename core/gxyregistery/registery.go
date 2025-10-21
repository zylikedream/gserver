package gxyregistery

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/container/gmap"
	"github.com/gogf/gf/v2/net/gsvc"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/pkg/errors"
)

const (
	REGISTERY_TYPE_ETCD   = "etcd"
	REGISTERY_TYPE_CONSUL = "consul"
)

type Services struct {
	Infos   []*ServiceInfo
	Version int
}

type IRegistery interface {
	Register(ctx context.Context, service *ServiceInfo) error
	Search(ctx context.Context, name string) ([]*ServiceInfo, error)
	UnRegister(ctx context.Context, service *ServiceInfo) error
}

type registery struct {
	gsvc.Registry
	services *gmap.StrAnyMap
}

func NewRegistery(t string, config string) (*registery, error) {
	var err error
	var r gsvc.Registry
	switch t {
	case REGISTERY_TYPE_ETCD:
		r, err = newEtcdRegistery(config)
		if err != nil {
			return nil, err
		}
	case REGISTERY_TYPE_CONSUL:
		r, err = newConsulRegistery(config)
	default:
		return nil, errors.Errorf("not support registery type %s", t)
	}
	if err != nil {
		return nil, err
	}
	return &registery{
		Registry: r,
		services: gmap.NewStrAnyMap(true),
	}, nil
}

func (r *registery) Register(ctx context.Context, svcInfo *ServiceInfo) error {
	_, err := r.Registry.Register(ctx, svcInfo)
	return err
}

func (r *registery) UnRegister(ctx context.Context, svcInfo *ServiceInfo) error {
	return r.Registry.Deregister(ctx, svcInfo)
}

func (r *registery) Search(ctx context.Context, name string) ([]*ServiceInfo, error) {
	services, err := r.Registry.Search(ctx, gsvc.SearchInput{
		Name: name,
	})
	if err != nil {
		return nil, err
	}
	serviceInfos := r.toServiceInfos(services)
	if !r.services.Contains(name) {
		r.services.Set(name, serviceInfos)
		go r.StartWatch(name)
	}
	return serviceInfos, nil

}

func (r *registery) toServiceInfo(svc gsvc.Service) *ServiceInfo {
	if svc.GetValue() == "" {
		return nil
	}
	sv, err := NewServiceFromBytes([]byte(svc.GetValue()))
	if err != nil {
		glog.Errorf(context.Background(), "NewServiceFromBytes err: %v", err)
		return nil
	}
	return sv
}

func (r *registery) toServiceInfos(svcs []gsvc.Service) []*ServiceInfo {
	serviceInfos := []*ServiceInfo{}
	for _, svc := range svcs {
		sv := r.toServiceInfo(svc)
		if sv == nil {
			continue
		}
		serviceInfos = append(serviceInfos, sv)
	}
	return serviceInfos
}

// compareServiceInfos 比较两个服务信息列表是否相等
func (r *registery) compareServiceInfos(old, new []*ServiceInfo) bool {
	if len(old) != len(new) {
		return false
	}

	// 创建映射以便快速查找
	oldMap := make(map[string]*ServiceInfo)
	for _, svc := range old {
		oldMap[svc.GetKey()] = svc
	}

	for _, newSvc := range new {
		oldSvc, exists := oldMap[newSvc.GetKey()]
		if !exists {
			return false
		}
		// 比较关键信息是否变化
		if oldSvc.GetVersion() != newSvc.GetVersion() ||
			oldSvc.GetEndpoints()[0].Host() != newSvc.GetEndpoints()[0].Host() ||
			oldSvc.GetEndpoints()[0].Port() != newSvc.GetEndpoints()[0].Port() {
			return false
		}
	}
	return true
}

func (r *registery) StartWatch(name string) {
	watcher, err := r.Registry.Watch(context.Background(), name)
	if err != nil {
		glog.Errorf(context.Background(), "Watch err: %v", err)
		return
	}

	// 服务变更防抖定时器
	var debounceTimer *time.Timer
	var pendingServices []*ServiceInfo
	debounceDuration := 2 * time.Second

	// 防抖处理函数
	processDebouncedUpdate := func() {
		if pendingServices != nil {
			glog.Infof(context.Background(), "Updating services for %s, count: %d", name, len(pendingServices))
			r.services.Set(name, pendingServices)
			pendingServices = nil
		}
	}

	for {
		services, err := watcher.Proceed()
		if err != nil {
			glog.Errorf(context.Background(), "Proceed err: %v", err)
			// 添加重试延迟，避免错误风暴
			time.Sleep(time.Second)
			continue
		}

		serviceInfos := r.toServiceInfos(services)

		// 获取当前服务列表进行比较，避免不必要的更新
		currentServices := r.services.Get(name)
		if currentServices != nil {
			currentSvcList, ok := currentServices.([]*ServiceInfo)
			if ok && r.compareServiceInfos(currentSvcList, serviceInfos) {
				// 服务信息没有实际变化，跳过更新
				glog.Debugf(context.Background(), "No actual change in services for %s, skipping update", name)
				continue
			}
		}

		// 使用防抖机制，合并短时间内的多次变更
		pendingServices = serviceInfos

		if debounceTimer != nil {
			debounceTimer.Stop()
		}

		debounceTimer = time.AfterFunc(debounceDuration, processDebouncedUpdate)
	}
}
