package gxyactor

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"

	"gserver/core/gxylog"
	"gserver/core/gxyredis"
	"gserver/core/gxyregistery"
)

type benchServiceLookup struct {
	services []*gxyregistery.ServiceInfo
}

func (b *benchServiceLookup) GetAddressByNodeName(_ context.Context, _ string, nodeInstanceName string) string {
	for _, svc := range b.services {
		if svc.NodeName == nodeInstanceName {
			return svc.NodeHost
		}
	}
	return ""
}

func (b *benchServiceLookup) GetServiceInfo(ctx context.Context, name string, key string, selector gxyregistery.ServiceSelector) *gxyregistery.ServiceInfo {
	hs := gxyregistery.HashServices{ServiceInfos: b.services, Hash: "bench"}
	return selector.Select(ctx, name, key, hs)
}

func benchRedisReady(b *testing.B) {
	b.Helper()
	if os.Getenv("RUN_REDIS_TESTS") != "1" {
		b.Skip("set RUN_REDIS_TESTS=1 to run Redis benchmarks")
	}
	f, err := os.CreateTemp("", "redis-bench-*.toml")
	if err != nil {
		b.Fatalf("create temp config error: %v", err)
	}
	if _, err := fmt.Fprint(f, `[redis]
addr = "127.0.0.1:6379"
dial_timeout = "1s"
`); err != nil {
		_ = f.Close()
		b.Fatalf("write temp config error: %v", err)
	}
	if err := f.Close(); err != nil {
		b.Fatalf("close temp config error: %v", err)
	}
	g.Cfg().GetAdapter().(*gcfg.AdapterFile).SetFileName(f.Name())
	redisApp := gxyredis.NewRedisApp()
	if err := redisApp.OnModInit(context.Background()); err != nil {
		b.Skipf("redis test config unavailable: %v", err)
	}
	if err := redisApp.OnModStart(context.Background()); err != nil {
		b.Skipf("redis unavailable: %v", err)
	}
	b.Cleanup(func() {
		_ = redisApp.OnModStop(context.Background())
	})
}

func BenchmarkRegisterActorLocate(b *testing.B) {
	benchRedisReady(b)
	mgr := NewActivatorManager("bench", "bench@1")
	if err := mgr.locator.acquireNodeLease(context.Background()); err != nil {
		b.Fatal(err)
	}
	keyPrefix := "bench-player"

	b.ResetTimer()
	i := 0
	for b.Loop() {
		i++
		id := fmt.Sprintf("%s-%d", keyPrefix, i)
		if _, _, err := mgr.locator.claim(context.Background(), "role", id); err != nil {
			b.Fatalf("claim actor owner error = %v", err)
		}
	}
}

func BenchmarkGetActorLocateNodeName(b *testing.B) {
	benchRedisReady(b)
	key := getActorLocateKey("role", "bench-player")
	leaseKey := actorLocatorLeaseKey("bench@node")
	if err := gxyredis.Redis().Set(context.Background(), key, "bench@node|1|bench-token", 0).Err(); err != nil {
		b.Fatalf("setup locate key error = %v", err)
	}
	if err := gxyredis.Redis().Set(context.Background(), leaseKey, "bench-token", actorLocateLeaseTTL).Err(); err != nil {
		b.Fatalf("setup lease key error = %v", err)
	}
	b.Cleanup(func() {
		gxyredis.Redis().Del(context.Background(), key, leaseKey)
	})

	b.ResetTimer()
	for b.Loop() {
		if _, err := getActorLocateNodeName(context.Background(), "role", "bench-player"); err != nil {
			b.Fatalf("getActorLocateNodeName() error = %v", err)
		}
	}
}

func BenchmarkGetActorHitWith1000Nodes(b *testing.B) {
	benchRedisReady(b)
	services := make([]*gxyregistery.ServiceInfo, 0, 1000)
	targetNode := ""
	targetHost := ""
	for i := 0; i < 1000; i++ {
		nodeName := fmt.Sprintf("role-sim-%04d", i+1)
		nodeHost := fmt.Sprintf("10.0.0.%d:19000", i+1)
		services = append(services, gxyregistery.NewServiceInfo("role", nodeName, nodeHost, "sim", 1))
		if i == 777 {
			targetNode = nodeName
			targetHost = nodeHost
		}
	}

	mgr := NewActivatorManager("bench", "bench@1")
	mgr.serviceLookup = &benchServiceLookup{services: services}
	key := getActorLocateKey("role", "bench-player")
	leaseKey := actorLocatorLeaseKey(targetNode)
	if err := gxyredis.Redis().Set(context.Background(), key, encodeActorOwner(ActorOwner{NodeID: targetNode, Epoch: 1}, "bench-token"), 0).Err(); err != nil {
		b.Fatalf("setup locate key error = %v", err)
	}
	if err := gxyredis.Redis().Set(context.Background(), leaseKey, "bench-token", actorLocateLeaseTTL).Err(); err != nil {
		b.Fatalf("setup lease key error = %v", err)
	}
	b.Cleanup(func() {
		gxyredis.Redis().Del(context.Background(), key, leaseKey)
	})

	b.ResetTimer()
	for b.Loop() {
		pid, err := mgr.getActor(context.Background(), "role", "bench-player", false)
		if err != nil {
			b.Fatalf("getActor() error = %v", err)
		}
		if pid == nil {
			b.Fatal("getActor() returned nil pid")
		}
		if pid.Address != targetHost {
			b.Fatalf("pid address = %q, want %q", pid.Address, targetHost)
		}
	}
}

func BenchmarkGetActorMissWith1000Nodes(b *testing.B) {
	benchRedisReady(b)
	services := make([]*gxyregistery.ServiceInfo, 0, 1000)
	for i := 0; i < 1000; i++ {
		nodeName := fmt.Sprintf("role-sim-%04d", i+1)
		nodeHost := fmt.Sprintf("10.0.0.%d:19000", i+1)
		services = append(services, gxyregistery.NewServiceInfo("role", nodeName, nodeHost, "sim", 1))
	}
	hs := gxyregistery.HashServices{ServiceInfos: services, Hash: "bench"}
	selector := gxyregistery.ConsistentHashSelector()
	gxylog.SetLevel("error")
	routeKey := getActorLocateKey("role", "bench-player")
	expected := selector.Select(context.Background(), "role", routeKey, hs)
	if expected == nil {
		b.Fatal("expected selector returned nil")
	}

	mgr := NewActivatorManager("bench", "bench@1")
	mgr.serviceLookup = &benchServiceLookup{services: services}
	mgr.requestActorFunc = func(_ context.Context, node string, _ string, id string, _ bool) (PID, bool, error) {
		return actor.NewPID(node, id), false, nil
	}

	b.ResetTimer()
	for b.Loop() {
		pid, err := mgr.getActor(context.Background(), "role", "bench-player", true)
		if err != nil {
			b.Fatalf("getActor() error = %v", err)
		}
		if pid == nil {
			b.Fatal("getActor() returned nil pid")
		}
		if pid.Address != expected.NodeHost {
			b.Fatalf("pid address = %q, want %q", pid.Address, expected.NodeHost)
		}
	}
}

func BenchmarkGetAddressByNodeNameWith1000Nodes(b *testing.B) {
	services := make([]*gxyregistery.ServiceInfo, 0, 1000)
	targetNode := ""
	for i := 0; i < 1000; i++ {
		nodeName := fmt.Sprintf("role-sim-%04d", i+1)
		nodeHost := fmt.Sprintf("10.0.0.%d:19000", i+1)
		services = append(services, gxyregistery.NewServiceInfo("role", nodeName, nodeHost, "sim", 1))
		if i == 777 {
			targetNode = nodeName
		}
	}
	lookup := &benchServiceLookup{services: services}

	b.ResetTimer()
	for b.Loop() {
		if got := lookup.GetAddressByNodeName(context.Background(), "role", targetNode); got == "" {
			b.Fatal("GetAddressByNodeName returned empty address")
		}
	}
}

func BenchmarkConsistentHashSelectWith1000Nodes(b *testing.B) {
	ctx := context.Background()
	services := make([]*gxyregistery.ServiceInfo, 0, 1000)
	for i := 0; i < 1000; i++ {
		nodeName := fmt.Sprintf("role-sim-%04d", i+1)
		nodeHost := fmt.Sprintf("10.0.0.%d:19000", i+1)
		services = append(services, gxyregistery.NewServiceInfo("role", nodeName, nodeHost, "sim", 1))
	}
	hs := gxyregistery.HashServices{ServiceInfos: services, Hash: "bench"}
	selector := gxyregistery.ConsistentHashSelector()
	gxylog.SetLevel("error")

	b.ResetTimer()
	for b.Loop() {
		if got := selector.Select(ctx, "role", "bench-player", hs); got == nil {
			b.Fatal("selector returned nil")
		}
	}
}

func BenchmarkConsistentHashSelectColdWith1000Nodes(b *testing.B) {
	ctx := context.Background()
	services := make([]*gxyregistery.ServiceInfo, 0, 1000)
	for i := 0; i < 1000; i++ {
		nodeName := fmt.Sprintf("role-sim-%04d", i+1)
		nodeHost := fmt.Sprintf("10.0.0.%d:19000", i+1)
		services = append(services, gxyregistery.NewServiceInfo("role", nodeName, nodeHost, "sim", 1))
	}
	selector := gxyregistery.ConsistentHashSelector()
	gxylog.SetLevel("error")

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		hs := gxyregistery.HashServices{
			ServiceInfos: services,
			Hash:         fmt.Sprintf("bench-cold-%d", i),
		}
		if got := selector.Select(ctx, "role", "bench-player", hs); got == nil {
			b.Fatal("selector returned nil")
		}
	}
}
