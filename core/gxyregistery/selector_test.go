package gxyregistery

import (
	"context"
	"fmt"
	"math"
	"testing"
)

func makeServiceInfo(name, nodeName, nodeHost string) *ServiceInfo {
	return NewServiceInfo(name, nodeName, nodeHost, "v1.0.0", 0)
}

func TestConsistentHashDistribution(t *testing.T) {
	// 模拟生产环境：2 个 role pod，100 个虚拟节点
	selector := ConsistentHashSelectorWithVirtualNodes(100)
	svcs := HashServices{
		ServiceInfos: []*ServiceInfo{
			makeServiceInfo("role", "role-0", "10.244.0.43:19001"),
			makeServiceInfo("role", "role-1", "10.244.0.38:19001"),
		},
		Hash: "1",
	}

	// 模拟 2000 个 roleID（和压测一样的 key 格式）
	n := 2000
	counts := map[string]int{}
	for i := range n {
		roleID := 100002 + i
		key := fmt.Sprintf("gserver:locate:node:actor:role:%d", roleID)
		svc := selector.Select(context.Background(), "role", key, svcs)
		if svc == nil {
			t.Fatal("select returned nil")
		}
		counts[svc.NodeName]++
	}

	got0 := counts["role-0"]
	got1 := counts["role-1"]
	total := got0 + got1

	t.Logf("=== ConsistentHash 分布测试 (100 虚拟节点, %d key) ===", n)
	t.Logf("role-0: %d (%.1f%%)", got0, float64(got0)/float64(total)*100)
	t.Logf("role-1: %d (%.1f%%)", got1, float64(got1)/float64(total)*100)

	// 预期均匀分布，允许 10% 偏差
	expected := n / 2
	margin := float64(expected) * 0.10
	if math.Abs(float64(got0-expected)) > margin {
		t.Errorf("distribution too skewed: got %d vs %d (expected ~%d)", got0, got1, expected)
	}
}

func TestConsistentHashSelectorSkipsDrainingServices(t *testing.T) {
	selector := ConsistentHashSelectorWithVirtualNodes(10)
	draining := makeServiceInfo("role", "role-0", "10.244.0.43:19001")
	draining.State = ServiceStateDraining
	serving := makeServiceInfo("role", "role-1", "10.244.0.38:19001")
	serving.State = ServiceStateServing
	svcs := HashServices{
		ServiceInfos: []*ServiceInfo{draining, serving},
		Hash:         "1",
	}

	for i := range 100 {
		key := fmt.Sprintf("role:%d", i)
		svc := selector.Select(context.Background(), "role", key, svcs)
		if svc == nil {
			t.Fatal("select returned nil")
		}
		if svc.NodeName != "role-1" {
			t.Fatalf("expected serving node role-1, got %s", svc.NodeName)
		}
	}
}

func TestSelectorReturnsNilWhenNoServingServices(t *testing.T) {
	selector := ConsistentHashSelectorWithVirtualNodes(10)
	draining := makeServiceInfo("role", "role-0", "10.244.0.43:19001")
	draining.State = ServiceStateDraining
	maintaining := makeServiceInfo("role", "role-1", "10.244.0.38:19001")
	maintaining.State = ServiceStateMaintaining
	svcs := HashServices{
		ServiceInfos: []*ServiceInfo{draining, maintaining},
		Hash:         "1",
	}

	svc := selector.Select(context.Background(), "role", "role:1", svcs)
	if svc != nil {
		t.Fatalf("expected nil when all services are unavailable, got %s", svc.NodeName)
	}
}
