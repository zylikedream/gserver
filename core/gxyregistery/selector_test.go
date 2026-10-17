package gxyregistery

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"

	"gserver/core/gxyregistery/types"
)

func makeServiceInfo(name, nodeName, nodeHost string) *types.ServiceInfo {
	return types.NewServiceInfo(name, nodeName, nodeHost, "v1.0.0", 0)
}

func TestConsistentHashDistribution(t *testing.T) {
	// 模拟生产环境：2 个 role pod，100 个虚拟节点
	selector := ConsistentHashSelectorWithVirtualNodes(100)
	svcs := HashServices{
		ServiceInfos: []*types.ServiceInfo{
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
	draining.State = types.ServiceStateDraining
	serving := makeServiceInfo("role", "role-1", "10.244.0.38:19001")
	serving.State = types.ServiceStateServing
	svcs := HashServices{
		ServiceInfos: []*types.ServiceInfo{draining, serving},
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
	draining.State = types.ServiceStateDraining
	maintaining := makeServiceInfo("role", "role-1", "10.244.0.38:19001")
	maintaining.State = types.ServiceStateMaintaining
	svcs := HashServices{
		ServiceInfos: []*types.ServiceInfo{draining, maintaining},
		Hash:         "1",
	}

	svc := selector.Select(context.Background(), "role", "role:1", svcs)
	if svc != nil {
		t.Fatalf("expected nil when all services are unavailable, got %s", svc.NodeName)
	}
}

// TestSelectorConcurrentSafety 选择器在请求路径上被多协程共享,共享状态必须并发安全。
// 覆盖三个曾经的缺陷:
//   - serviceIndex 用 gmap 的 safe=false 构造(裸 map,并发读写触发 fatal error:
//     concurrent map read and map write);
//   - round-robin 用 singleflight 合并同 key 并发调用,导致所有请求拿到同一下标;
//   - 一致性哈希环被原地 Clear+Set,并发读者会遍历到残缺的环。
func TestSelectorConcurrentSafety(t *testing.T) {
	svcs := []*types.ServiceInfo{
		makeServiceInfo("role", "role-0", "10.244.0.43:19001"),
		makeServiceInfo("role", "role-1", "10.244.0.38:19001"),
	}

	t.Run("round_robin_distributes_concurrent_calls", func(t *testing.T) {
		sel := RoundRobinSelector()
		h := HashServices{ServiceInfos: svcs, Hash: "1"}

		const callers = 64
		picked := make([]string, callers)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range callers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				// 同一服务名并发:singleflight 版本会让所有调用拿到同一个下标
				picked[i] = sel.Select(context.Background(), "role", "k", h).NodeName
			}()
		}
		close(start)
		wg.Wait()

		distinct := map[string]bool{}
		for _, n := range picked {
			distinct[n] = true
		}
		if len(distinct) != len(svcs) {
			t.Fatalf("并发轮询只命中 %d 个节点, want %d(全部):%v", len(distinct), len(svcs), distinct)
		}
	})

	// 两版成员集交替出现(模拟成员变化),并发 Select 必须始终返回"调用方那一版"
	// 的成员:原地重建的环会被读者看到残缺态,从而取到另一版的节点。
	t.Run("consistent_hash_returns_node_of_queried_version", func(t *testing.T) {
		sel := ConsistentHashSelectorWithVirtualNodes(100)

		verA := HashServices{Hash: "a", ServiceInfos: []*types.ServiceInfo{
			makeServiceInfo("role", "role-0", "10.244.0.43:19001"),
			makeServiceInfo("role", "role-1", "10.244.0.38:19001"),
		}}
		verB := HashServices{Hash: "b", ServiceInfos: []*types.ServiceInfo{
			makeServiceInfo("role", "role-0", "10.244.0.43:19001"),
			makeServiceInfo("role", "role-2", "10.244.0.39:19001"),
		}}
		allowed := func(h HashServices, n *types.ServiceInfo) bool {
			for _, svc := range h.ServiceInfos {
				if svc.NodeName == n.NodeName {
					return true
				}
			}
			return false
		}

		const callers = 64
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range callers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				h := verA
				if i%2 == 1 {
					h = verB
				}
				got := sel.Select(context.Background(), "role", fmt.Sprintf("key-%d", i), h)
				if got == nil {
					t.Error("Select 返回 nil")
					return
				}
				if !allowed(h, got) {
					t.Errorf("返回 %s, 不属于查询的版本 %s", got.NodeName, h.Hash)
				}
			}()
		}
		close(start)
		wg.Wait()
	})
}
