package gxyregistery

import (
	"context"
	"gserver/util"
	"sort"
	"time"

	"github.com/gogf/gf/v2/container/gmap"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/gsvc"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/gconv"
	"github.com/pkg/errors"
)

const (
	REGISTERY_TYPE_ETCD   = "etcd"
	REGISTERY_TYPE_CONSUL = "consul"
)

type IRegistery interface {
	Register(ctx context.Context, service *ServiceInfo) error
	Search(ctx context.Context, name string) ([]*ServiceInfo, error)
	UnRegister(ctx context.Context, service *ServiceInfo) error
	GetHashServices(ctx context.Context, name string) (HashServices, error)
}

type registery struct {
	gsvc.Registry
	services *gmap.StrAnyMap
	seqs     *gmap.StrIntMap
}

func NewRegistery() (*registery, error) {
	cfg := g.Cfg()
	t := cfg.MustGet(context.Background(), "registery.type").String()
	var err error
	var r gsvc.Registry
	switch t {
	case REGISTERY_TYPE_ETCD:
		r, err = newEtcdRegistery(cfg)
		if err != nil {
			return nil, err
		}
	case REGISTERY_TYPE_CONSUL:
		r, err = newConsulRegistery(cfg)
	default:
		return nil, errors.Errorf("not support registery type %s", t)
	}
	if err != nil {
		return nil, err
	}
	return &registery{
		Registry: r,
		services: gmap.NewStrAnyMap(true),
		seqs:     gmap.NewStrIntMap(true),
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
	// 对服务列表进行排序，确保返回顺序一致
	// 排序依据：服务名称 > 节点主机 > 版本号
	sort.Slice(serviceInfos, func(i, j int) bool {
		if serviceInfos[i].Name != serviceInfos[j].Name {
			return serviceInfos[i].Name < serviceInfos[j].Name
		}
		if serviceInfos[i].NodeHost != serviceInfos[j].NodeHost {
			return serviceInfos[i].NodeHost < serviceInfos[j].NodeHost
		}
		if serviceInfos[i].Version != serviceInfos[j].Version {
			return serviceInfos[i].Version < serviceInfos[j].Version
		}
		return serviceInfos[i].NodeName < serviceInfos[j].NodeName
	})

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
			r.updateServices(name, pendingServices)
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

func (r *registery) updateServices(name string, services []*ServiceInfo) {
	r.services.Set(name, services)
	newSeq := r.seqs.GetOrSet(name, 0) + 1
	r.seqs.Set(name, newSeq)
	glog.Infof(context.Background(), "Updating services for %s, count: %d, seq: %d, services: %s", name,
		len(services), newSeq, util.FormatObject(services))
}

func (r *registery) GetHashServices(ctx context.Context, name string) (HashServices, error) {
	var err error
	services, ok := r.services.Get(name).([]*ServiceInfo)
	if !ok || len(services) == 0 {
		services, err = r.Search(ctx, name)
		if err != nil {
			return HashServices{}, err
		}
		r.updateServices(name, services)
	}
	seq := r.seqs.GetOrSet(name, 0)
	return HashServices{
		ServiceInfos: services,
		Hash:         gconv.String(seq),
	}, nil
}
