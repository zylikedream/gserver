package gxyregistery

import (
	"context"
	"crypto/md5"
	"fmt"
	"gserver/core/gxylog"
	"gserver/core/gxyutil"
	"math/rand"

	"github.com/gogf/gf/v2/container/gmap"
	"github.com/gogf/gf/v2/container/gtree"
	"golang.org/x/sync/singleflight"
)

type ServiceSelector interface {
	Select(ctx context.Context, service string, key string, services HashServices) *ServiceInfo
}

type randomServiceSelector struct {
}

var randomSelector = &randomServiceSelector{}

func RandomSelector() ServiceSelector {
	return randomSelector
}

func (s *randomServiceSelector) Select(ctx context.Context, service string, _ string, hservices HashServices) *ServiceInfo {
	services := servingServices(hservices.ServiceInfos)
	if len(services) == 0 {
		return nil
	}
	return services[rand.Intn(len(services))]
}

type roundRobinServiceSelector struct {
	serviceIndex *gmap.StrIntMap
	sfg          singleflight.Group
}

var roundRobinSelector = &roundRobinServiceSelector{
	serviceIndex: gmap.NewStrIntMap(false),
}

func RoundRobinSelector() ServiceSelector {
	return roundRobinSelector
}

func (s *roundRobinServiceSelector) Select(ctx context.Context, service string, _ string, hservices HashServices) *ServiceInfo {
	services := servingServices(hservices.ServiceInfos)
	if len(services) == 0 {
		return nil
	}
	// 使用singleflight.Group确保每个服务的索引更新是原子的
	val, _, _ := s.sfg.Do(service, func() (any, error) {
		index := s.serviceIndex.GetOrSet(service, 0)
		s.serviceIndex.Set(service, (index+1)%len(services))
		return index, nil
	})
	return services[val.(int)]
}

// consistentHashSelector 实现基于一致性哈希的服务选择器
// 一致性哈希算法可以在节点数量变化时最小化键的重新映射
// 通过虚拟节点机制提高哈希分布的均匀性

type consistentHashSelector struct {
	// 每个服务节点对应的一致性哈希环
	rings *gmap.StrAnyMap
	hashs *gmap.StrStrMap
	// 真实节点映射的虚拟节点数量
	virtualNodeCount int
}

var consistentHashSelectorInstance = ConsistentHashSelectorWithVirtualNodes(100)

// ConsistentHashSelector 返回一致性哈希选择器实例
func ConsistentHashSelector() ServiceSelector {
	return consistentHashSelectorInstance
}

// ConsistentHashSelectorWithVirtualNodes 返回自定义虚拟节点数量的一致性哈希选择器
func ConsistentHashSelectorWithVirtualNodes(count int) ServiceSelector {
	return &consistentHashSelector{
		rings:            gmap.NewStrAnyMap(true),
		hashs:            gmap.NewStrStrMap(false),
		virtualNodeCount: count,
	}
}

// Select 根据一致性哈希算法选择一个服务节点
// 使用service作为key计算哈希值，在哈希环上找到对应的节点
func (s *consistentHashSelector) Select(ctx context.Context, service string, key string, hservices HashServices) *ServiceInfo {
	services := servingServices(hservices.ServiceInfos)
	if len(services) == 0 {
		return nil
	}

	ring := s.ring(service)
	if hashval := s.hashs.Get(service); hashval == "" || hashval != hservices.Hash {
		s.rebuildRing(ring, services)
		s.hashs.Set(service, hservices.Hash)
		gxylog.Debug(ctx, "consistentHashSelector rebuild ring",
			gxylog.Str("ring", service), gxylog.Str("hash", hservices.Hash),
			gxylog.Str("services", gxyutil.FormatObject(services)))
	}

	if node := pickRingNode(ring, s.hash(key)); node != nil {
		return node
	}

	gxylog.Warn(context.Background(), "consistentHashSelector Select no node found, return random node")
	return services[rand.Intn(len(services))]
}

// ring 返回该服务的哈希环,不存在则创建。
func (s *consistentHashSelector) ring(ringKey string) *gtree.AVLTree {
	if val := s.rings.Get(ringKey); val != nil {
		return val.(*gtree.AVLTree)
	}
	var ring *gtree.AVLTree
	s.rings.LockFunc(func(m map[string]any) {
		// 双重检查，防止在获取锁的过程中其他协程已经创建
		if val, exists := m[ringKey]; exists {
			ring = val.(*gtree.AVLTree)
			return
		}
		ring = gtree.NewAVLTree(compareHashKey)
		m[ringKey] = ring
	})
	return ring
}

// compareHashKey 按哈希值大小比较 AVL 树节点键。
func compareHashKey(a, b any) int {
	hashA := a.(uint32)
	hashB := b.(uint32)
	switch {
	case hashA < hashB:
		return -1
	case hashA > hashB:
		return 1
	default:
		return 0
	}
}

// pickRingNode 在环上选第一个哈希值 >= hash 的节点;没有则取第一个节点(形成环回)。
func pickRingNode(ring *gtree.AVLTree, hash uint32) *ServiceInfo {
	var selected *ServiceInfo
	ring.IteratorAsc(func(key, value any) bool {
		if key.(uint32) >= hash {
			selected = value.(*ServiceInfo)
			return false
		}
		return true
	})
	if selected != nil {
		return selected
	}
	ring.IteratorAsc(func(_, value any) bool {
		selected = value.(*ServiceInfo)
		return false
	})
	return selected
}

func servingServices(services []*ServiceInfo) []*ServiceInfo {
	filtered := make([]*ServiceInfo, 0, len(services))
	for _, svc := range services {
		if svc != nil && svc.IsServing() {
			filtered = append(filtered, svc)
		}
	}
	return filtered
}

// rebuildRing 重建一致性哈希环
func (s *consistentHashSelector) rebuildRing(ring *gtree.AVLTree, services []*ServiceInfo) {
	// 清空现有哈希环
	ring.Clear()

	// 为每个节点创建多个虚拟节点，并将它们添加到哈希环上
	for _, node := range services {
		for j := 0; j < s.virtualNodeCount; j++ {
			// 为虚拟节点生成唯一标识
			virtualNodeKey := fmt.Sprintf("%s:%s:%d", node.Name, node.NodeHost, j)
			// 计算虚拟节点的哈希值
			hash := s.hash(virtualNodeKey)
			// 添加到哈希环
			ring.Set(hash, node)
		}
	}
}

// hash 计算字符串的哈希值
func (s *consistentHashSelector) hash(key string) uint32 {
	// 使用MD5计算哈希值
	hash := md5.Sum([]byte(key))
	// 将16字节的MD5哈希转换为32位无符号整数
	return uint32(hash[0])<<24 | uint32(hash[1])<<16 | uint32(hash[2])<<8 | uint32(hash[3])
}
