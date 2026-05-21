package rolenodesim

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"gserver/core/gxyregistery"
)

type fakeRegistry struct {
	mu         sync.Mutex
	registered []*gxyregistery.ServiceInfo
}

func (f *fakeRegistry) Register(_ context.Context, service *gxyregistery.ServiceInfo) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered = append(f.registered, service)
	return nil
}

func TestBuildServicesGeneratesUniqueRoleNodes(t *testing.T) {
	opts := Options{
		Count:       3,
		ServiceName: "role",
		NodePrefix:  "role-sim",
		HostPrefix:  "127.0.0.1",
		StartPort:   19000,
		Version:     "sim",
		Weight:      1,
	}

	services, err := BuildServices(opts)
	if err != nil {
		t.Fatalf("BuildServices() error = %v", err)
	}
	if len(services) != 3 {
		t.Fatalf("BuildServices() len = %d, want 3", len(services))
	}

	for i, svc := range services {
		wantNode := fmt.Sprintf("%s-%04d", opts.NodePrefix, i+1)
		wantHost := fmt.Sprintf("%s:%d", opts.HostPrefix, opts.StartPort+i)

		if svc.Name != opts.ServiceName {
			t.Fatalf("service[%d].Name = %q, want %q", i, svc.Name, opts.ServiceName)
		}
		if svc.NodeName != wantNode {
			t.Fatalf("service[%d].NodeName = %q, want %q", i, svc.NodeName, wantNode)
		}
		if svc.NodeHost != wantHost {
			t.Fatalf("service[%d].NodeHost = %q, want %q", i, svc.NodeHost, wantHost)
		}
		if svc.Version != opts.Version {
			t.Fatalf("service[%d].Version = %q, want %q", i, svc.Version, opts.Version)
		}
		if svc.Weight != opts.Weight {
			t.Fatalf("service[%d].Weight = %d, want %d", i, svc.Weight, opts.Weight)
		}
	}
}

func TestRegisterAllRegistersEveryService(t *testing.T) {
	opts := Options{
		Count:       4,
		ServiceName: "role",
		NodePrefix:  "role-sim",
		HostPrefix:  "127.0.0.1",
		StartPort:   19000,
		Version:     "sim",
		Weight:      1,
		Concurrency: 2,
	}

	services, err := BuildServices(opts)
	if err != nil {
		t.Fatalf("BuildServices() error = %v", err)
	}

	reg := &fakeRegistry{}
	if err := RegisterAll(context.Background(), reg, services, opts.Concurrency); err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}

	if got := len(reg.registered); got != len(services) {
		t.Fatalf("RegisterAll() registered %d services, want %d", got, len(services))
	}

	seen := make(map[string]struct{}, len(services))
	for _, svc := range reg.registered {
		seen[svc.GetKey()] = struct{}{}
	}
	if len(seen) != len(services) {
		t.Fatalf("RegisterAll() registered %d unique services, want %d", len(seen), len(services))
	}
}
