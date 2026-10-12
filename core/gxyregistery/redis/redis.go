package redis

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxyredis"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/gsvc"
)

const (
	redisServiceKeyPrefix = "gserver:svc"
	defaultPollInterval   = 10 * time.Second
	defaultFieldTTL       = 30 * time.Second
	heartbeatInterval     = 20 * time.Second
)

// Registry implements gsvc.Registry using Redis + DNS.
// Service data is stored in Redis Hash with field-level TTL,
// NodeHost already contains the correct pod IP set via POD_IP at registration time.
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

// New returns a new DNS registry.
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
	w := &watcher{
		registry: r,
		name:     key,
		stopCh:   make(chan struct{}),
	}
	return w, nil
}

// Type returns the registry type name.
func (r *Registry) Type() string {
	return "redis"
}

// ---- heartbeat ----

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

func (r *Registry) renewHeartbeats(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for key, fields := range r.heartbeats {
		if len(fields) == 0 {
			continue
		}
		r.setFieldsTTL(ctx, key, slices.Collect(maps.Keys(fields))...)
	}
}

// ---- service adapter ----

// serviceData is the JSON structure stored in Redis (matching gxyregistery.ServiceData).
type serviceData struct {
	Name     string `json:"Name"`
	NodeName string `json:"NodeName"`
	Version  string `json:"Version"`
	Weight   int    `json:"Weight"`
	NodeHost string `json:"NodeHost"`
}

// redisService adapts serviceData to the gsvc.Service interface.
type redisService struct {
	serviceData
	jsonStr string
	key     string
}

func (s *redisService) GetName() string            { return s.Name }
func (s *redisService) GetVersion() string         { return s.Version }
func (s *redisService) GetKey() string             { return s.key }
func (s *redisService) GetValue() string           { return s.jsonStr }
func (s *redisService) GetPrefix() string          { return gsvc.DefaultSeparator + s.Name }
func (s *redisService) GetMetadata() gsvc.Metadata { return nil }
func (s *redisService) GetEndpoints() gsvc.Endpoints {
	return gsvc.NewEndpoints(s.NodeHost)
}

// ---- helper ----

func serviceFromJSON(jsonStr string) (gsvc.Service, error) {
	var data serviceData
	if err := gjson.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, err
	}

	if data.NodeName == "" {
		return nil, gerror.New("empty node name in service data")
	}

	// data.NodeHost already contains the correct pod IP (set during registration via POD_IP),
	// no need for extra DNS resolution.
	return &redisService{
		serviceData: data,
		jsonStr:     jsonStr,
		key:         buildServiceKey(data.Name, data.NodeName, data.NodeHost),
	}, nil
}

func buildServiceKey(name, nodeName, nodeHost string) string {
	return fmt.Sprintf("gserver-%s-%s:%s", nodeName, name, nodeHost)
}

// ---- watcher ----

type watcher struct {
	registry *Registry
	name     string
	stopCh   chan struct{}
	mu       sync.Mutex
	lastHash string
}

func (w *watcher) Proceed() ([]gsvc.Service, error) {
	ticker := time.NewTicker(w.registry.interval)
	defer ticker.Stop()
	ctx := context.Background()

	for {
		select {
		case <-ticker.C:
			services, hash, err := w.fetchWithHash(ctx)
			if err != nil {
				gxylog.Warn(ctx, "redis watcher fetch error", gxylog.Err(err))
				continue
			}
			w.mu.Lock()
			changed := hash != w.lastHash
			if changed {
				w.lastHash = hash
			}
			w.mu.Unlock()
			if changed {
				return services, nil
			}
		case <-w.stopCh:
			return nil, gerror.New("watcher stopped")
		}
	}
}

func (w *watcher) Close() error {
	close(w.stopCh)
	return nil
}

func (w *watcher) fetchWithHash(ctx context.Context) ([]gsvc.Service, string, error) {
	services, err := w.registry.Search(ctx, gsvc.SearchInput{Name: w.name})
	if err != nil {
		return nil, "", err
	}
	var hash strings.Builder
	fmt.Fprintf(&hash, "%d", len(services))
	for _, s := range services {
		hash.WriteString(s.GetKey())
		hash.WriteString(s.GetValue())
	}
	return services, hash.String(), nil
}
