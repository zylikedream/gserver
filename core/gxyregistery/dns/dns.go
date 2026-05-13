package dns

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"gserver/core/gxyredis"
	"gserver/core/gxylog"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/gsvc"
)

const (
	dnsServiceKeyPrefix = "gserver:dns:svc"
	defaultPollInterval = 10 * time.Second
	defaultFieldTTL     = 30 * time.Second
	heartbeatInterval   = 20 * time.Second
)

// Registry implements gsvc.Registry using Redis + DNS.
type Registry struct {
	domain   string
	interval time.Duration

	mu         sync.Mutex
	heartbeats map[string]map[string]struct{} // key → set of fields to renew
	stopCh     chan struct{}
	started    bool
}

// New returns a new DNS registry.
func New(domain string, interval time.Duration) *Registry {
	if interval <= 0 {
		interval = defaultPollInterval
	}
	return &Registry{
		domain:     domain,
		interval:   interval,
		heartbeats: make(map[string]map[string]struct{}),
		stopCh:     make(chan struct{}),
	}
}

func hashKey(svcName string) string {
	return fmt.Sprintf("%s:%s", dnsServiceKeyPrefix, svcName)
}

// Register stores the service in a Redis Hash with field-level TTL.
func (r *Registry) Register(ctx context.Context, service gsvc.Service) (gsvc.Service, error) {
	podName := extractPodName(service)
	key := hashKey(service.GetName())
	if err := gxyredis.Redis().HSet(ctx, key, podName, service.GetValue()).Err(); err != nil {
		return nil, err
	}
	r.setFieldTTL(ctx, key, podName)
	r.trackHeartbeat(key, podName)
	return service, nil
}

// Deregister removes the service from Redis and stops heartbeat.
func (r *Registry) Deregister(ctx context.Context, service gsvc.Service) error {
	podName := extractPodName(service)
	key := hashKey(service.GetName())
	r.untrackHeartbeat(key, podName)
	return gxyredis.Redis().HDel(ctx, key, podName).Err()
}

// Search returns all services of the given name from Redis Hash, resolving pod IPs via DNS.
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
	for podName, jsonStr := range fields {
		if jsonStr == "" {
			continue
		}
		svc, err := serviceFromJSON(jsonStr, podName, r.domain, in.Name)
		if err != nil {
			gxylog.Warn(ctx, "dns registry parse failed", gxylog.Err(err))
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
	return "dns"
}

// ---- heartbeat ----

// setFieldTTL sets field-level TTL on the hash field (Redis 7.4+ HEXPIRE).
func (r *Registry) setFieldTTL(ctx context.Context, key, field string) {
	ttl := int(defaultFieldTTL.Seconds())
	if err := gxyredis.Redis().Do(ctx, "HEXPIRE", key, ttl, "FIELDS", 1, field).Err(); err != nil {
		gxylog.Warn(context.Background(), "dns set field ttl failed",
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
			gxylog.Warn(context.Background(), "dns heartbeat renew failed",
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

// dnsService adapts serviceData to the gsvc.Service interface.
type dnsService struct {
	serviceData
	jsonStr string
	key     string
}

func (s *dnsService) GetName() string             { return s.serviceData.Name }
func (s *dnsService) GetVersion() string          { return s.serviceData.Version }
func (s *dnsService) GetKey() string              { return s.key }
func (s *dnsService) GetValue() string            { return s.jsonStr }
func (s *dnsService) GetPrefix() string           { return gsvc.DefaultSeparator + s.serviceData.Name }
func (s *dnsService) GetMetadata() gsvc.Metadata  { return nil }
func (s *dnsService) GetEndpoints() gsvc.Endpoints { return gsvc.NewEndpoints(s.serviceData.NodeHost) }

// ---- helper ----

// extractPodName extracts the pod name from the service's NodeName field.
// The ServiceInfo stores NodeName as "game-0@uid", we take the part before "@".
func extractPodName(svc gsvc.Service) string {
	// We need to get the NodeName. For our ServiceInfo, the key is:
	// "gserver-{NodeName}-{Name}:{NodeHost}"
	key := svc.GetKey()
	parts := strings.SplitN(key, "-", 2)
	if len(parts) < 2 {
		return ""
	}
	// parts[1] = "game-0@uid-role:0.0.0.0:10090"
	// After the first "-", find the LAST "-" and take everything before it
	rest := parts[1]
	idx := strings.LastIndex(rest, "-")
	if idx < 0 {
		return ""
	}
	nodeInfo := rest[:idx]
	nodeParts := strings.SplitN(nodeInfo, "@", 2)
	if len(nodeParts) != 2 {
		return nodeInfo
	}
	return nodeParts[0]
}

func serviceFromJSON(jsonStr string, podName string, domain string, svcName string) (gsvc.Service, error) {
	var data serviceData
	if err := gjson.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, err
	}

	if podName == "" {
		return nil, gerror.New("cannot extract pod name, empty field")
	}

	host, port, err := splitHostPort(data.NodeHost)
	if err != nil {
		return nil, err
	}

	// Build per-service DNS domain from the configured domain + service name.
	// e.g., domain="game-svc.default.svc.cluster.local", svcName="role"
	//       → "role-svc.default.svc.cluster.local"
	svcDomain := perServiceDomain(domain, svcName)
	resolvedIP, err := resolvePod(podName, svcDomain)
	if err != nil {
		gxylog.Warn(context.Background(), "dns resolve failed, using original host",
			gxylog.Str("pod", podName), gxylog.Str("domain", svcDomain), gxylog.Err(err))
	} else {
		host = resolvedIP
	}

	data.NodeHost = fmt.Sprintf("%s:%s", host, port)
	// Re-serialize so GetValue() returns the resolved address for registery.toServiceInfo.
	updatedJSON, _ := gjson.Marshal(data)
	return &dnsService{
		serviceData: data,
		jsonStr:     string(updatedJSON),
		key:         buildServiceKey(data.Name, data.NodeName, data.NodeHost),
	}, nil
}

func perServiceDomain(domain, svcName string) string {
	if domain == "" {
		return ""
	}
	// domain = "game-svc.default.svc.cluster.local"
	// Extract ".default.svc.cluster.local" and prepend "{svcName}-svc"
	parts := strings.SplitN(domain, ".", 2)
	if len(parts) < 2 {
		return domain
	}
	return svcName + "-svc." + parts[1]
}

func buildServiceKey(name, nodeName, nodeHost string) string {
	return fmt.Sprintf("gserver-%s-%s:%s", nodeName, name, nodeHost)
}

func splitHostPort(hostport string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(hostport)
	if err != nil {
		return hostport, "", nil
	}
	return
}

func resolvePod(podName, domain string) (string, error) {
	addr := podName
	if domain != "" {
		addr = podName + "." + domain
	}
	ips, err := net.DefaultResolver.LookupHost(context.Background(), addr)
	if err != nil {
		return "", err
	}
	if len(ips) == 0 {
		return "", gerror.New("no IP found for " + addr)
	}
	return ips[0], nil
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
				gxylog.Warn(context.Background(), "dns watcher fetch error", gxylog.Err(err))
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
