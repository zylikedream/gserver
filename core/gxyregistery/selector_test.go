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
	for i := 0; i < n; i++ {
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

func TestConsistentHashVirtualNodeImpact(t *testing.T) {
	// 对比不同虚拟节点数量对分布的影响
	for _, vn := range []int{5, 10, 20, 50, 100, 200} {
		selector := ConsistentHashSelectorWithVirtualNodes(vn)
		svcs := HashServices{
			ServiceInfos: []*ServiceInfo{
				makeServiceInfo("role", "role-0", "10.244.0.43:19001"),
				makeServiceInfo("role", "role-1", "10.244.0.38:19001"),
			},
			Hash: "1",
		}

		n := 2000
		counts := map[string]int{}
		for i := 0; i < n; i++ {
			roleID := 100002 + i
			key := fmt.Sprintf("gserver:locate:node:actor:role:%d", roleID)
			svc := selector.Select(context.Background(), "role", key, svcs)
			counts[svc.NodeName]++
		}

		got0 := counts["role-0"]
		got1 := counts["role-1"]
		skew := math.Abs(float64(got0-got1)) / float64(n) * 100
		t.Logf("virtualNodes=%2d  role-0=%4d  role-1=%4d  skew=%.1f%%", vn, got0, got1, skew)
	}
}

func TestConsistentHashRebalance(t *testing.T) {
	// 模拟节点从 1 个扩容到 2 个时的 rebalance 情况
	ctx := context.Background()

	// 阶段 1：只有 1 个节点
	selector := ConsistentHashSelectorWithVirtualNodes(10)
	svcs1 := HashServices{
		ServiceInfos: []*ServiceInfo{
			makeServiceInfo("role", "role-0", "10.244.0.43:19001"),
		},
		Hash: "0",
	}

	// 先分配 1000 个 actor 到 1 节点
	n := 2000
	firstRound := n / 2
	assignments := map[int]string{}
	for i := 0; i < firstRound; i++ {
		roleID := 100002 + i
		key := fmt.Sprintf("gserver:locate:node:actor:role:%d", roleID)
		svc := selector.Select(ctx, "role", key, svcs1)
		assignments[roleID] = svc.NodeName
	}

	// 阶段 2：扩容到 2 个节点（模拟 role-1 加入）
	svcs2 := HashServices{
		ServiceInfos: []*ServiceInfo{
			makeServiceInfo("role", "role-0", "10.244.0.43:19001"),
			makeServiceInfo("role", "role-1", "10.244.0.38:19001"),
		},
		Hash: "1",
	}

	// 再分配剩余的 actor 到扩容后的 2 节点
	for i := firstRound; i < n; i++ {
		roleID := 100002 + i
		key := fmt.Sprintf("gserver:locate:node:actor:role:%d", roleID)
		svc := selector.Select(ctx, "role", key, svcs2)
		assignments[roleID] = svc.NodeName
	}

	finalCounts := map[string]int{}
	for _, node := range assignments {
		finalCounts[node]++
	}
	t.Logf("=== 扩容模拟 (先 1 节点分配 1000，再扩容到 2 节点分配剩余 1000) ===")
	t.Logf("role-0: %d (%.1f%%)", finalCounts["role-0"], float64(finalCounts["role-0"])/float64(n)*100)
	t.Logf("role-1: %d (%.1f%%)", finalCounts["role-1"], float64(finalCounts["role-1"])/float64(n)*100)
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

	for i := 0; i < 100; i++ {
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
