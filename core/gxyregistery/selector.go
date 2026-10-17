package gxyregistery

import (
	"context"
	"crypto/md5"
	"fmt"
	"sync"
	"sync/atomic"

	"gserver/core/gxylog"
	"gserver/core/gxyregistery/types"
	"gserver/core/gxyutil"
	"math/rand"

	"github.com/gogf/gf/v2/container/gmap"
	"github.com/gogf/gf/v2/container/gtree"
)

type ServiceSelector interface {
	Select(ctx context.Context, service string, key string, services HashServices) *types.ServiceInfo
}

type randomServiceSelector struct {
}

var randomSelector = &randomServiceSelector{}

func RandomSelector() ServiceSelector {
	return randomSelector
}

func (s *randomServiceSelector) Select(ctx context.Context, service string, _ string, hservices HashServices) *types.ServiceInfo {
	services := servingServices(hservices.ServiceInfos)
	if len(services) == 0 {
		return nil
	}
	return services[rand.Intn(len(services))]
}

// roundRobinServiceSelector 按服务名各持一个计数器,轮流返回成员。
// 计数器用 atomic 而非 singleflight:singleflight 会把同 key 并发调用合并成
// 同一个结果,导致并发下所有请求拿到同一下标(轮询退化为固定打一个节点)。
type roundRobinServiceSelector struct {
	indexes sync.Map // service → *atomic.Int64
}

var roundRobinSelector = &roundRobinServiceSelector{}

func RoundRobinSelector() ServiceSelector {
	return roundRobinSelector
}

func (s *roundRobinServiceSelector) Select(ctx context.Context, service string, _ string, hservices HashServices) *types.ServiceInfo {
	services := servingServices(hservices.ServiceInfos)
	if len(services) == 0 {
		return nil
	}
	counter, _ := s.indexes.LoadOrStore(service, new(atomic.Int64))
	n := counter.(*atomic.Int64).Add(1) - 1
	return services[int(n%int64(len(services)))]
}

// consistentHashSelector 实现基于一致性哈希的服务选择器
// 一致性哈希算法可以在节点数量变化时最小化键的重新映射
// 通过虚拟节点机制提高哈希分布的均匀性

// ringEntry 是一份哈希环快照:构建完成后 ring 不再被修改,成员变化时整体替换
// map 中的指针。读者要么看到旧快照、要么看到新快照,不存在中间态 —— 原地
// Clear+Set 会让并发读者遍历到一个残缺的环,取到不属于当前成员集的节点。
type ringEntry struct {
	ring *gtree.AVLTree
	hash string
}

type consistentHashSelector struct {
	// entries: service → *ringEntry
	entries *gmap.StrAnyMap
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
		entries:          gmap.NewStrAnyMap(true),
		virtualNodeCount: count,
	}
}

// Select 根据一致性哈希算法选择一个服务节点
// 使用service作为key计算哈希值，在哈希环上找到对应的节点
func (s *consistentHashSelector) Select(ctx context.Context, service string, key string, hservices HashServices) *types.ServiceInfo {
	services := servingServices(hservices.ServiceInfos)
	if len(services) == 0 {
		return nil
	}

	// 环与版本号成对读取:分成两次读会拿到"新版本号配旧环",从而漏掉重建
	entry, _ := s.entries.Get(service).(*ringEntry)
	if entry == nil || entry.hash != hservices.Hash {
		entry = &ringEntry{ring: s.buildRing(services), hash: hservices.Hash}
		s.entries.Set(service, entry)
		gxylog.Debug(ctx, "consistentHashSelector rebuild ring",
			gxylog.Str("ring", service), gxylog.Str("hash", hservices.Hash),
			gxylog.Str("services", gxyutil.FormatObject(services)))
	}

	// 用局部变量 entry.ring,不重新回 map 取:保证"校验过的版本"与"查询的环"同源
	if node := pickRingNode(entry.ring, s.hash(key)); node != nil {
		return node
	}

	gxylog.Warn(ctx, "consistentHashSelector Select no node found, return random node")
	return services[rand.Intn(len(services))]
}

// buildRing 为给定成员集构建新的一致性哈希环。构建在旁路进行,不影响正在使用
// 旧快照的读者;构建完成后此环不再被修改。
// 传 safe=true 是防御性的:环本应只读,万一将来有人误改,加锁能兜住。
func (s *consistentHashSelector) buildRing(services []*types.ServiceInfo) *gtree.AVLTree {
	ring := gtree.NewAVLTree(compareHashKey, true)
	for _, node := range services {
		for j := range s.virtualNodeCount {
			// 虚拟节点标识:同一真实节点映射出多个环上位置,改善分布均匀性
			ring.Set(s.hash(fmt.Sprintf("%s:%s:%d", node.Name, node.NodeHost, j)), node)
		}
	}
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
func pickRingNode(ring *gtree.AVLTree, hash uint32) *types.ServiceInfo {
	var selected *types.ServiceInfo
	ring.IteratorAsc(func(key, value any) bool {
		if key.(uint32) >= hash {
			selected = value.(*types.ServiceInfo)
			return false
		}
		return true
	})
	if selected != nil {
		return selected
	}
	ring.IteratorAsc(func(_, value any) bool {
		selected = value.(*types.ServiceInfo)
		return false
	})
	return selected
}

func servingServices(services []*types.ServiceInfo) []*types.ServiceInfo {
	filtered := make([]*types.ServiceInfo, 0, len(services))
	for _, svc := range services {
		if svc != nil && svc.IsServing() {
			filtered = append(filtered, svc)
		}
	}
	return filtered
}

// hash 计算字符串的哈希值
func (s *consistentHashSelector) hash(key string) uint32 {
	// 使用MD5计算哈希值
	hash := md5.Sum([]byte(key))
	// 将16字节的MD5哈希转换为32位无符号整数
	return uint32(hash[0])<<24 | uint32(hash[1])<<16 | uint32(hash[2])<<8 | uint32(hash[3])
}
