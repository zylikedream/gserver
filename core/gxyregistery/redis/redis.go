package redis

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gserver/core/gxyredis"
	"gserver/core/gxylog"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/gsvc"
)

const (
	redisServiceKeyPrefix = "gserver:svc"
	defaultPollInterval = 10 * time.Second
	defaultFieldTTL     = 30 * time.Second
	heartbeatInterval   = 20 * time.Second
)

// Registry implements gsvc.Registry using Redis + DNS.
// Service data is stored in Redis Hash with field-level TTL,
// NodeHost already contains the correct pod IP set via POD_IP at registration time.
type Registry struct {
	interval time.Duration

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
	if err := gxyredis.Redis().HSet(ctx, key, field, service.GetValue()).Err(); err != nil {
		return nil, err
	}
	r.setFieldTTL(ctx, key, field)
	r.trackHeartbeat(key, field)
	return service, nil
}

// Deregister removes the service from Redis and stops heartbeat.
func (r *Registry) Deregister(ctx context.Context, service gsvc.Service) error {
	key := hashKey(service.GetName())
	field := service.GetKey()
	r.untrackHeartbeat(key, field)
	return gxyredis.Redis().HDel(ctx, key, field).Err()
}

// Search returns all services of the given name from Redis Hash.
func (r *Registry) Search(ctx context.Context, in gsvc.SearchInput) ([]gsvc.Service, error) {
	key := hashKey(in.Name)
	fields, err := gxyredis.Redis().HGetAll(ctx, key).Result()
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

// setFieldTTL sets field-level TTL on the hash field (Redis 7.4+ HEXPIRE).
func (r *Registry) setFieldTTL(ctx context.Context, key, field string) {
	ttl := int(defaultFieldTTL.Seconds())
	if err := gxyredis.Redis().Do(ctx, "HEXPIRE", key, ttl, "FIELDS", 1, field).Err(); err != nil {
		gxylog.Warn(context.Background(), "redis set field ttl failed",
			gxylog.Str("key", key), gxylog.Str("field", field), gxylog.Err(err))
	}
}

func (r *Registry) trackHeartbeat(key, field string) {
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

func (r *Registry) untrackHeartbeat(key, field string) {
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

	for {
		select {
		case <-ticker.C:
			r.renewHeartbeats()
		case <-r.stopCh:
			return
		}
	}
}

func (r *Registry) renewHeartbeats() {
	r.mu.Lock()
	defer r.mu.Unlock()

	ttl := int(defaultFieldTTL.Seconds())
	for key, fields := range r.heartbeats {
		if len(fields) == 0 {
			continue
		}
		fieldSlice := make([]string, 0, len(fields))
		for f := range fields {
			fieldSlice = append(fieldSlice, f)
		}
		args := make([]any, 5+len(fieldSlice))
		args[0] = "HEXPIRE"
		args[1] = key
		args[2] = ttl
		args[3] = "FIELDS"
		args[4] = len(fieldSlice)
		for i, f := range fieldSlice {
			args[5+i] = f
		}
		if err := gxyredis.Redis().Do(context.Background(), args...).Err(); err != nil {
			gxylog.Warn(context.Background(), "redis heartbeat renew failed",
				gxylog.Str("key", key), gxylog.Err(err))
		}
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

func (s *redisService) GetName() string             { return s.serviceData.Name }
func (s *redisService) GetVersion() string          { return s.serviceData.Version }
func (s *redisService) GetKey() string              { return s.key }
func (s *redisService) GetValue() string            { return s.jsonStr }
func (s *redisService) GetPrefix() string           { return gsvc.DefaultSeparator + s.serviceData.Name }
func (s *redisService) GetMetadata() gsvc.Metadata  { return nil }
func (s *redisService) GetEndpoints() gsvc.Endpoints { return gsvc.NewEndpoints(s.serviceData.NodeHost) }

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

	for {
		select {
		case <-ticker.C:
			services, hash, err := w.fetchWithHash()
			if err != nil {
				gxylog.Warn(context.Background(), "redis watcher fetch error", gxylog.Err(err))
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

func (w *watcher) fetchWithHash() ([]gsvc.Service, string, error) {
	services, err := w.registry.Search(context.Background(), gsvc.SearchInput{Name: w.name})
	if err != nil {
		return nil, "", err
	}
	hash := fmt.Sprintf("%d", len(services))
	for _, s := range services {
		hash += s.GetKey()
	}
	return services, hash, nil
}
