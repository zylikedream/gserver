package rolenodesim

import (
	"context"
	"fmt"
	"io"
	"time"

	"gserver/core/gxyregistery"

	"golang.org/x/sync/errgroup"
)

const (
	defaultCount       = 1000
	defaultServiceName = "role"
	defaultNodePrefix  = "role-sim"
	defaultHostPrefix  = "127.0.0.1"
	defaultStartPort   = 19000
	defaultVersion     = "sim"
	defaultWeight      = 1
	defaultConcurrency = 32
)

// Registrar registers services into a registry backend.
type Registrar interface {
	Register(context.Context, *gxyregistery.ServiceInfo) error
}

// Lifecycle extends Registrar with unregister support.
type Lifecycle interface {
	Registrar
	UnRegister(context.Context, *gxyregistery.ServiceInfo) error
}

// Options controls the role-node simulation.
type Options struct {
	Count       int
	ServiceName string
	NodePrefix  string
	HostPrefix  string
	StartPort   int
	Version     string
	Weight      int
	Concurrency int
	Duration    time.Duration
}

func (o Options) withDefaults() Options {
	if o.Count <= 0 {
		o.Count = defaultCount
	}
	if o.ServiceName == "" {
		o.ServiceName = defaultServiceName
	}
	if o.NodePrefix == "" {
		o.NodePrefix = defaultNodePrefix
	}
	if o.HostPrefix == "" {
		o.HostPrefix = defaultHostPrefix
	}
	if o.StartPort <= 0 {
		o.StartPort = defaultStartPort
	}
	if o.Version == "" {
		o.Version = defaultVersion
	}
	if o.Weight == 0 {
		o.Weight = defaultWeight
	}
	if o.Concurrency <= 0 {
		o.Concurrency = defaultConcurrency
	}
	return o
}

// BuildServices creates a deterministic set of fake role services.
func BuildServices(opts Options) ([]*gxyregistery.ServiceInfo, error) {
	opts = opts.withDefaults()
	services := make([]*gxyregistery.ServiceInfo, 0, opts.Count)
	for i := 0; i < opts.Count; i++ {
		nodeName := fmt.Sprintf("%s-%04d", opts.NodePrefix, i+1)
		nodeHost := fmt.Sprintf("%s:%d", opts.HostPrefix, opts.StartPort+i)
		services = append(services, gxyregistery.NewServiceInfo(
			opts.ServiceName,
			nodeName,
			nodeHost,
			opts.Version,
			opts.Weight,
		))
	}
	return services, nil
}

// RegisterAll registers every service with bounded concurrency.
func RegisterAll(ctx context.Context, reg Registrar, services []*gxyregistery.ServiceInfo, concurrency int) error {
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}

	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(concurrency)
	for _, service := range services {
		svc := service
		group.Go(func() error {
			return reg.Register(ctx, svc)
		})
	}
	return group.Wait()
}

// UnRegisterAll removes all services from the registry.
func UnRegisterAll(ctx context.Context, reg Lifecycle, services []*gxyregistery.ServiceInfo, concurrency int) error {
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(concurrency)
	for _, service := range services {
		svc := service
		group.Go(func() error {
			return reg.UnRegister(ctx, svc)
		})
	}
	return group.Wait()
}

// Run registers the simulated role nodes, waits for shutdown, and unregisters them.
func Run(ctx context.Context, reg Lifecycle, opts Options, out io.Writer) error {
	opts = opts.withDefaults()

	services, err := BuildServices(opts)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "registering %d simulated role nodes\n", len(services)); err != nil {
		return err
	}
	if err := RegisterAll(ctx, reg, services, opts.Concurrency); err != nil {
		_ = UnRegisterAll(context.Background(), reg, services, opts.Concurrency)
		return err
	}
	if _, err := fmt.Fprintf(out, "registered %d simulated role nodes\n", len(services)); err != nil {
		return err
	}

	if opts.Duration > 0 {
		timer := time.NewTimer(opts.Duration)
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	} else {
		select {
		case <-ctx.Done():
		}
	}

	if _, err := fmt.Fprintln(out, "unregistering simulated role nodes"); err != nil {
		return err
	}
	return UnRegisterAll(context.Background(), reg, services, opts.Concurrency)
}
