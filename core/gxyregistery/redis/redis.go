package redis

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"sync"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxyredis"
	"gserver/core/gxyregistery/types"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/gsvc"
)

const (
	redisServiceKeyPrefix = "gserver:svc"
	// defaultPollInterval 是 watcher 轮询间隔,同时决定发现侧最长延迟。
	defaultPollInterval = 10 * time.Second
	// defaultFieldTTL 是字段过期时间。HEXPIRE 是重置而非累加,且对已不存在的
	// 字段返回 0、不会复活 —— 字段一旦过期就是永久消失(要重新 Register 才回来),
	// 所以必须给足漏拍余量:TTL/heartbeat = 3,连续漏 2 拍(40s 持续失败)才过期。
	// 反向代价是崩溃节点最长 60s 才从发现中消失,由调用方请求超时兜底,量级与
	// consul 后端的 DeregisterCriticalServiceAfter=1m 一致。
	defaultFieldTTL   = 60 * time.Second
	heartbeatInterval = 20 * time.Second
)

// Registry 用 Redis Hash 实现 gsvc.Registry:
// 每个服务名一个 hash key,每个实例一个 field,过期靠字段级 TTL 自然淘汰。
// NodeHost 在注册时已通过 POD_IP 写成真实地址,无需 DNS 解析。
type Registry struct {
	interval time.Duration
	// client 惰性取用缓存客户端:进程内单例可能晚于本对象初始化。
	// 组装根用默认值,测试注入 mock(见 invariants.md「测试替身规则」)。
	client func() gxyredis.Client

	mu         sync.Mutex
	heartbeats map[string]map[string]struct{} // key → set of fields to renew
	stopCh     chan struct{}
	started    bool
}

// New 返回一个轮询式的服务注册表。
func New(interval time.Duration) *Registry {
	if interval <= 0 {
		interval = defaultPollInterval
	}
	return &Registry{
		interval:   interval,
		client:     gxyredis.Redis,
		heartbeats: make(map[string]map[string]struct{}),
		stopCh:     make(chan struct{}),
	}
}

func hashKey(svcName string) string {
	return fmt.Sprintf("%s:%s", redisServiceKeyPrefix, svcName)
}

// Register stores the service in a Redis Hash with field-level TTL.
func (r *Registry) Register(ctx context.Context, service gsvc.Service) (gsvc.Service, error) {
	key := hashKey(service.GetName())
	field := service.GetKey()
	if err := r.client().HSet(ctx, key, field, service.GetValue()).Err(); err != nil {
		return nil, err
	}
	r.startHeartbeat(ctx, key, field)
	return service, nil
}

// Deregister removes the service from Redis and stops heartbeat.
func (r *Registry) Deregister(ctx context.Context, service gsvc.Service) error {
	key := hashKey(service.GetName())
	field := service.GetKey()
	r.stopHeartbeat(key, field)
	return r.client().HDel(ctx, key, field).Err()
}

// Search returns all services of the given name from Redis Hash.
func (r *Registry) Search(ctx context.Context, in gsvc.SearchInput) ([]gsvc.Service, error) {
	key := hashKey(in.Name)
	fields, err := r.client().HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return []gsvc.Service{}, nil
	}

	var services []gsvc.Service
	for _, jsonStr := range fields {
		if jsonStr == "" {
			continue
		}
		svc, err := serviceFromJSON(jsonStr)
		if err != nil {
			gxylog.Warn(ctx, "redis registry parse failed", gxylog.Err(err))
			continue
		}
		services = append(services, svc)
	}
	sort.Slice(services, func(i, j int) bool {
		return services[i].GetKey() < services[j].GetKey()
	})
	return services, nil
}

// Watch watches for service changes via polling.
func (r *Registry) Watch(ctx context.Context, key string) (gsvc.Watcher, error) {
	return &watcher{registry: r, name: key}, nil
}

// setFieldsTTL 给给定字段设置字段级 TTL(Redis 7.4+ HEXPIRE)。
func (r *Registry) setFieldsTTL(ctx context.Context, key string, fields ...string) {
	if err := r.client().HExpire(ctx, key, defaultFieldTTL, fields...).Err(); err != nil {
		gxylog.Warn(ctx, "redis set field ttl failed",
			gxylog.Str("key", key), gxylog.Err(err))
	}
}

// startHeartbeat 开始为字段续期:设置初始 TTL,并纳入周期续期列表。
// 首次调用会启动续期协程。
func (r *Registry) startHeartbeat(ctx context.Context, key, field string) {
	r.setFieldsTTL(ctx, key, field)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.heartbeats[key] == nil {
		r.heartbeats[key] = make(map[string]struct{})
	}
	r.heartbeats[key][field] = struct{}{}

	if !r.started {
		r.started = true
		go r.heartbeatLoop()
	}
}

// stopHeartbeat 把字段移出续期列表。
func (r *Registry) stopHeartbeat(key, field string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if fields, ok := r.heartbeats[key]; ok {
		delete(fields, field)
		if len(fields) == 0 {
			delete(r.heartbeats, key)
		}
	}
}

func (r *Registry) heartbeatLoop() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	ctx := context.Background()

	for {
		select {
		case <-ticker.C:
			r.renewHeartbeats(ctx)
		case <-r.stopCh:
			return
		}
	}
}

// renewHeartbeats 批量续期。先在锁内快照键与字段,再在锁外发 HEXPIRE,
// 避免 redis 慢查询阻塞 Register/Deregister 路径。
func (r *Registry) renewHeartbeats(ctx context.Context) {
	r.mu.Lock()
	type target struct {
		key    string
		fields []string
	}
	targets := make([]target, 0, len(r.heartbeats))
	for key, fields := range r.heartbeats {
		if len(fields) == 0 {
			continue
		}
		targets = append(targets, target{key: key, fields: slices.Collect(maps.Keys(fields))})
	}
	r.mu.Unlock()

	for _, t := range targets {
		r.setFieldsTTL(ctx, t.key, t.fields...)
	}
}

// ---- service adapter ----

// serviceFromJSON 从存储值还原服务描述符。
// NodeHost 在注册时已通过 POD_IP 写成真实地址,无需额外 DNS 解析。
func serviceFromJSON(jsonStr string) (gsvc.Service, error) {
	svc, err := types.NewServiceFromBytes([]byte(jsonStr))
	if err != nil {
		return nil, err
	}
	if svc.NodeName == "" {
		return nil, gerror.New("empty node name in service data")
	}
	return svc, nil
}

// ---- watcher ----

type watcher struct {
	registry *Registry
	name     string
}

// Proceed 轮询一次并返回当前成员。
// 变化判定由调用方 registery.StartWatch 的 compareServiceInfos 负责,这里不重复实现。
func (w *watcher) Proceed() ([]gsvc.Service, error) {
	ticker := time.NewTicker(w.registry.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			return w.registry.Search(context.Background(), gsvc.SearchInput{Name: w.name})
		}
	}
}

// Close 由 gsvc.Watcher 接口要求。轮询由 Proceed 的调用方驱动,
// 本类型没有独立后台资源,故为空实现。
func (w *watcher) Close() error { return nil }
