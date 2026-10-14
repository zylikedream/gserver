package gxyregistery

import (
	"context"

	"gserver/core/gxylog"
	"gserver/core/gxyregistery/types"
	"gserver/core/gxyutil"
	"sort"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/gogf/gf/v2/container/gmap"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/gsvc"
	"github.com/gogf/gf/v2/util/gconv"
)

const (
	REGISTERY_TYPE_ETCD   = "etcd"
	REGISTERY_TYPE_CONSUL = "consul"
	REGISTERY_TYPE_REDIS  = "redis"
)

type IRegistery interface {
	Register(ctx context.Context, service *types.ServiceInfo) error
	Search(ctx context.Context, name string) ([]*types.ServiceInfo, error)
	UnRegister(ctx context.Context, service *types.ServiceInfo) error
	GetHashServices(ctx context.Context, name string) (HashServices, error)
}

type registery struct {
	regist   gsvc.Registry
	services *gmap.StrAnyMap
	seqs     *gmap.StrIntMap
	// watchStarted 保证每个服务名只会启动一个 StartWatch 协程。
	// 缓存未命中时并发 Search 会重复触发启动,导致同一名字有多个 watcher
	// 同时消费后端并写缓存。按 name 而非全局:否则首个名字会堵死其余名字。
	watchStarted sync.Map
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
	case REGISTERY_TYPE_REDIS:
		r, err = newRedisRegistery(cfg)
	default:
		return nil, errors.Newf("not support registery type %s", t)
	}
	if err != nil {
		return nil, err
	}
	return &registery{
		regist:   r,
		services: gmap.NewStrAnyMap(true),
		seqs:     gmap.NewStrIntMap(true),
	}, nil
}

func (r *registery) Register(ctx context.Context, svcInfo *types.ServiceInfo) error {
	_, err := r.regist.Register(ctx, svcInfo)
	return err
}

func (r *registery) UnRegister(ctx context.Context, svcInfo *types.ServiceInfo) error {
	return r.regist.Deregister(ctx, svcInfo)
}

func (r *registery) Search(ctx context.Context, name string) ([]*types.ServiceInfo, error) {
	services, err := r.regist.Search(ctx, gsvc.SearchInput{
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

func (r *registery) toServiceInfo(svc gsvc.Service) *types.ServiceInfo {
	if svc.GetValue() == "" {
		return nil
	}
	sv, err := types.NewServiceFromBytes([]byte(svc.GetValue()))
	if err != nil {
		gxylog.Error(context.Background(), "types.NewServiceFromBytes failed", gxylog.Err(err))
		return nil
	}
	return sv
}

func (r *registery) toServiceInfos(svcs []gsvc.Service) []*types.ServiceInfo {
	serviceInfos := []*types.ServiceInfo{}
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
func (r *registery) compareServiceInfos(old, new []*types.ServiceInfo) bool {
	if len(old) != len(new) {
		return false
	}

	// 创建映射以便快速查找
	oldMap := make(map[string]*types.ServiceInfo)
	for _, svc := range old {
		oldMap[svc.GetKey()] = svc
	}

	for _, newSvc := range new {
		oldSvc, exists := oldMap[newSvc.GetKey()]
		if !exists {
			return false
		}
		// 比较关键信息是否变化
		if oldSvc.GetValue() != newSvc.GetValue() {
			return false
		}
	}
	return true
}

func (r *registery) StartWatch(name string) {
	// LoadOrStore 保证并发 Search 下每个名字只有第一个调用方启动协程
	if _, loaded := r.watchStarted.LoadOrStore(name, struct{}{}); loaded {
		return
	}
	// 退出即失效:哨兵与缓存条目一起清掉,让下一次 Search 能重建 watcher。
	// 只清哨兵不够 —— 缓存条目不失效则永远是"有效"的,Search 里 !Contains(name)
	// 再不为真,这个服务名就永久没有 watcher 了(缓存也冻在最后一次快照上)。
	defer func() {
		r.watchStarted.Delete(name)
		r.services.Remove(name)
	}()

	ctx := context.Background()
	watcher, err := r.regist.Watch(ctx, name)
	if err != nil {
		gxylog.Error(ctx, "watch failed", gxylog.Err(err))
		return
	}
	defer func() { _ = watcher.Close() }()

	for {
		services, err := watcher.Proceed()
		if err != nil {
			gxylog.Error(ctx, "watch proceed failed", gxylog.Err(err))
			return
		}

		serviceInfos := r.toServiceInfos(services)

		// 获取当前服务列表进行比较，避免不必要的更新
		currentServices := r.services.Get(name)
		if currentServices != nil {
			currentSvcList, ok := currentServices.([]*types.ServiceInfo)
			if ok && r.compareServiceInfos(currentSvcList, serviceInfos) {
				// 服务信息没有实际变化，跳过更新
				gxylog.Debug(ctx, "No actual change in services, skipping update", gxylog.Str("service", name))
				continue
			}
		}

		// 直接生效。上游已自带合并(consul watcher 的 eventChan 容量为 1 且非阻塞投递,
		// redis watcher 按轮询间隔返回),此处再加防抖会饿死:事件间隔小于防抖窗口时
		// 缓存永远不更新,而 GetHashServices 缓存非空后不回源查询。
		r.updateServices(ctx, name, serviceInfos)
	}
}

// updateServices 写入本地缓存并递增版本号,版本号即 HashServices.Hash,
// 消费方据此判断是否需要重建哈希环。
func (r *registery) updateServices(ctx context.Context, name string, services []*types.ServiceInfo) {
	r.services.Set(name, services)
	newSeq := r.seqs.GetOrSet(name, 0) + 1
	r.seqs.Set(name, newSeq)
	gxylog.Info(ctx, "Updating services", gxylog.Str("name", name),
		gxylog.Num("count", len(services)), gxylog.Num("seq", newSeq))
	// 全量成员列表仅调试时需要,故留在 Debug 级
	gxylog.Debug(ctx, "Updating services detail", gxylog.Str("name", name),
		gxylog.Str("services", gxyutil.FormatObject(services)))
}

func (r *registery) GetHashServices(ctx context.Context, name string) (HashServices, error) {
	var err error
	services, ok := r.services.Get(name).([]*types.ServiceInfo)
	if !ok || len(services) == 0 {
		services, err = r.Search(ctx, name)
		if err != nil {
			return HashServices{}, err
		}
		r.updateServices(ctx, name, services)
	}
	seq := r.seqs.GetOrSet(name, 0)
	return HashServices{
		ServiceInfos: services,
		Hash:         gconv.String(seq),
	}, nil
}
