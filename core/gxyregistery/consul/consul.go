package consul

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/gsvc"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/gconv"
)

const (
	// DEFAULT_TTL is the default TTL for service registration
	DEFAULT_TTL = 22 * time.Second

	// DEFAULT_HEALTH_CHECK_INTERVAL is the default interval for health check
	DEFAULT_HEALTH_CHECK_INTERVAL = 10 * time.Second
)

var (
	_ gsvc.Registry = (*Registry)(nil)
)

// Registry implements gsvc.Registry interface using consul.
type Registry struct {
	client              *api.Client                   // Consul client
	address             string                        // Consul address
	options             map[string]string             // Additional options
	healthCheckInterval time.Duration                 // Health check interval
	ttl                 time.Duration                 // TTL for service registration
	mu                  sync.RWMutex                  // Mutex for thread safety
	stopHealthCheck     map[string]context.CancelFunc // Stop signals for health check goroutines
	logger              *glog.Logger                  // Logger for logging
}

// Option is the configuration option type for registry.
type Option func(r *Registry)

// WithAddress sets the address for consul client.
func WithAddress(address string) Option {
	return func(r *Registry) {
		r.mu.Lock()
		r.address = address
		r.mu.Unlock()
	}
}

// WithToken sets the ACL token for consul client.
func WithToken(token string) Option {
	return func(r *Registry) {
		r.mu.Lock()
		r.options["token"] = token
		r.mu.Unlock()
	}
}

func WithHealthCheckInterval(interval time.Duration) Option {
	return func(r *Registry) {
		r.mu.Lock()
		r.healthCheckInterval = interval
		r.mu.Unlock()
	}
}

func WithTTL(ttl time.Duration) Option {
	return func(r *Registry) {
		r.mu.Lock()
		r.ttl = ttl
		r.mu.Unlock()
	}
}

func WithLogger(logger *glog.Logger) Option {
	return func(r *Registry) {
		r.mu.Lock()
		r.logger = logger
		r.mu.Unlock()
	}
}

// New creates and returns a new Registry.
func New(opts ...Option) (gsvc.Registry, error) {
	r := &Registry{
		address:             "127.0.0.1:8500",
		options:             make(map[string]string),
		healthCheckInterval: DEFAULT_HEALTH_CHECK_INTERVAL,
		ttl:                 DEFAULT_TTL,
		stopHealthCheck:     make(map[string]context.CancelFunc),
		logger:              glog.New(),
	}

	// Apply options
	for _, opt := range opts {
		opt(r)
	}

	// Create consul config
	config := api.DefaultConfig()
	r.mu.RLock()
	config.Address = r.address
	if token, ok := r.options["token"]; ok {
		config.Token = token
	}
	r.mu.RUnlock()

	// Create consul client
	client, err := api.NewClient(config)
	if err != nil {
		return nil, err
	}
	r.client = client

	return r, nil
}

// Register registers a service to consul.
func (r *Registry) Register(ctx context.Context, service gsvc.Service) (gsvc.Service, error) {

	// Create service ID
	serviceID := service.GetKey()
	if serviceID == "" {
		return nil, gerror.New("get serviceID failed")
	}
	if len(service.GetEndpoints()) == 0 {
		return nil, gerror.New("service endpoints empty")
	}

	metadata := service.GetMetadata()
	meta := make(map[string]string)
	for k, v := range metadata {
		meta[k] = gconv.String(v)
	}
	meta["data"] = service.GetValue()
	// Create registration
	reg := &api.AgentServiceRegistration{
		ID:      serviceID,
		Name:    service.GetName(),
		Tags:    []string{service.GetName(), service.GetVersion()},
		Meta:    meta,
		Address: service.GetEndpoints()[0].Host(),
		Port:    service.GetEndpoints()[0].Port(),
	}

	// Add health check
	// 优化TTL配置，确保心跳间隔小于TTL的2/3
	// 这样可以在网络波动时提供足够的缓冲
	checkID := fmt.Sprintf("service:%s", serviceID)
	// 计算合理的TTL，确保healthCheckInterval小于TTL的2/3
	adjustedTTL := max(r.ttl, r.healthCheckInterval*3/2)
	reg.Check = &api.AgentServiceCheck{
		CheckID:                        checkID,
		TTL:                            adjustedTTL.String(),
		DeregisterCriticalServiceAfter: "1m",
	}

	// Register service
	if err := r.client.Agent().ServiceRegister(reg); err != nil {
		return nil, gerror.Wrap(err, "failed to register service")
	}

	// Start TTL health check
	if err := r.client.Agent().PassTTL(checkID, ""); err != nil {
		// Try to deregister service if health check fails
		_ = r.client.Agent().ServiceDeregister(serviceID)
		return nil, gerror.Wrap(err, "failed to pass TTL health check")
	}

	// Start TTL health check goroutine
	ctx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.stopHealthCheck[serviceID] = cancel
	r.mu.Unlock()
	go r.ttlHealthCheck(ctx, serviceID, reg)

	return service, nil
}

// Deregister deregisters a service from consul.
func (r *Registry) Deregister(ctx context.Context, service gsvc.Service) error {
	serviceID := service.GetKey()
	if serviceID == "" {
		return gerror.New("get serviceID failed")
	}

	r.logger.Infof(ctx, "deregister service: %s", serviceID)

	// Stop health check goroutine first
	r.mu.Lock()
	if cancel, ok := r.stopHealthCheck[serviceID]; ok {
		cancel()
		delete(r.stopHealthCheck, serviceID)
	}
	r.mu.Unlock()

	// Deregister service
	if err := r.client.Agent().ServiceDeregister(serviceID); err != nil {
		return gerror.Wrap(err, "failed to deregister service")
	}

	return nil
}

// ttlHealthCheck maintains the TTL health check for a service
func (r *Registry) ttlHealthCheck(ctx context.Context, serviceID string, reg *api.AgentServiceRegistration) {
	ticker := time.NewTicker(r.healthCheckInterval)
	defer ticker.Stop()

	checkID := fmt.Sprintf("service:%s", serviceID)
	retryCount := 0
	maxRetries := 3
	retryInterval := time.Second * 2

	for {
		select {
		case <-ctx.Done():
			r.logger.Infof(context.Background(), "health check stopped for service: %s", serviceID)
			return
		case <-ticker.C:
		}

		if err := r.client.Agent().PassTTL(checkID, ""); err == nil {
			retryCount = 0
			continue
		} else {
			retryCount++
			r.logger.Errorf(context.Background(), "failed to pass TTL health check: %s, error %+v, retry count: %d", checkID, err, retryCount)

			// 连续失败达到阈值:TTL 可能已过期被 consul 摘除,重新注册挂回去。
			// 这个协程持有"本节点在 consul 里存活"的不变量,失败也不能退出 ——
			// 退出后 TTL 无人续期,consul 会在 DeregisterCriticalServiceAfter 后
			// 摘除本节点,且没有任何路径会重启它。
			if retryCount >= maxRetries {
				r.logger.Errorf(context.Background(), "max retries reached for health check: %s, try to re-register", checkID)
				if rerr := r.client.Agent().ServiceRegister(reg); rerr != nil {
					r.logger.Errorf(context.Background(), "failed to re-register service: %s, error %+v", checkID, rerr)
				} else {
					r.logger.Infof(context.Background(), "re-register service success: %s", checkID)
					retryCount = 0
				}
			}

			// 退避等待,可被停机中断;ticker 会丢弃这段时间的 tick,不会积压
			select {
			case <-ctx.Done():
				r.logger.Infof(context.Background(), "health check stopped for service: %s", serviceID)
				return
			case <-time.After(retryInterval):
			}
		}
	}
}

// GetAddress returns the consul address
func (r *Registry) GetAddress() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.address
}

// Watch creates and returns a watcher for specified service.
func (r *Registry) Watch(ctx context.Context, key string) (gsvc.Watcher, error) {
	return newWatcher(r, key)
}
