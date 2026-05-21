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
	// DefaultTTL is the default TTL for service registration
	DefaultTTL = 22 * time.Second

	// DefaultHealthCheckInterval is the default interval for health check
	DefaultHealthCheckInterval = 10 * time.Second
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
		healthCheckInterval: DefaultHealthCheckInterval,
		ttl:                 DefaultTTL,
		stopHealthCheck:     make(map[string]context.CancelFunc),
		logger:              glog.DefaultLogger(),
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
	adjustedTTL := r.ttl
	if adjustedTTL <= r.healthCheckInterval*3/2 {
		adjustedTTL = r.healthCheckInterval * 3 / 2
	}
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
	ctx, cancel := context.WithCancel(context.Background())
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
		err := r.client.Agent().PassTTL(checkID, "")
		if err != nil {
			r.logger.Errorf(context.Background(), "failed to pass TTL health check: %s, error %+v, retry count: %d", checkID, err, retryCount)
			retryCount++
			if retryCount <= maxRetries {
				time.Sleep(retryInterval)
				continue
			}
			// 达到最大重试次数，尝试重新注册服务
			r.logger.Errorf(context.Background(), "max retries reached for health check: %s, try to re-register", checkID)
			if err := r.client.Agent().ServiceRegister(reg); err != nil {
				r.logger.Errorf(context.Background(), "failed to re-register service: %s, error %+v", checkID, err)
				return
			}
			// 重新注册成功，重置重试计数继续健康检查
			r.logger.Infof(context.Background(), "re-register service success: %s", checkID)
			retryCount = 0
			continue
		}
		retryCount = 0
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
	watcher, err := newWatcher(r, key)
	if err != nil {
		return nil, err
	}
	return watcher, nil
}
