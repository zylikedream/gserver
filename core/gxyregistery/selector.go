package gxyregistery

import (
	"context"
	"crypto/md5"
	"fmt"
	"gserver/util"
	"math/rand"

	"github.com/gogf/gf/v2/container/gmap"
	"github.com/gogf/gf/v2/container/gtree"
	"github.com/gogf/gf/v2/os/glog"
	"golang.org/x/sync/singleflight"
)

type ServiceSelector interface {
	Select(service string, key string, services HashServices) *ServiceInfo
}

type randomServiceSelector struct {
}

var randomSelector = &randomServiceSelector{}

func RandomSelector() ServiceSelector {
	return randomSelector
}

func (s *randomServiceSelector) Select(service string, _ string, hservices HashServices) *ServiceInfo {
	services := hservices.ServiceInfos
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

func (s *roundRobinServiceSelector) Select(service string, _ string, hservices HashServices) *ServiceInfo {
	services := hservices.ServiceInfos
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

var consistentHashSelectorInstance = ConsistentHashSelectorWithVirtualNodes(10)

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
func (s *consistentHashSelector) Select(service string, key string, hservices HashServices) *ServiceInfo {
	services := hservices.ServiceInfos
	if len(services) == 0 {
		return nil
	}

	// 为当前服务获取或创建哈希环
	ringKey := service
	var ringObj any
	if val := s.rings.Get(ringKey); val != nil {
		ringObj = val
	} else {
		// 加锁创建新的哈希环
		s.rings.LockFunc(func(m map[string]any) {
			// 双重检查，防止在获取锁的过程中其他协程已经创建
			if val, exists := m[ringKey]; exists {
				ringObj = val
				return
			}
			// 创建新的有序树作为哈希环
			ring := gtree.NewAVLTree(func(a, b any) int {
				// 比较哈希值大小
				hashA := a.(uint32)
				hashB := b.(uint32)
				if hashA < hashB {
					return -1
				} else if hashA > hashB {
					return 1
				} else {
					return 0
				}
			})
			m[ringKey] = ring
			ringObj = ring
		})
	}
	ring := ringObj.(*gtree.AVLTree)
	hashval := s.hashs.Get(ringKey)

	if hashval == "" || hashval != hservices.Hash {
		s.rebuildRing(ring, services)
		s.hashs.Set(ringKey, hservices.Hash)
		glog.Debugf(context.Background(), "consistentHashSelector rebuild ring, ring: %s, hash: %s, services: %s",
			ringKey, hservices.Hash, util.FormatObject(services))
	}

	// 计算服务的哈希值
	hash := s.hash(key)

	// 在哈希环上查找大于等于当前哈希值的最小节点
	var selectedNode *ServiceInfo
	found := false

	// 遍历AVL树查找第一个大于等于当前哈希值的节点
	ring.IteratorAsc(func(key, value any) bool {
		currentHash := key.(uint32)
		if currentHash >= hash {
			selectedNode = value.(*ServiceInfo)
			found = true
			return false // 找到后停止遍历
		}
		return true // 继续遍历
	})

	// 如果没有找到，使用环上的第一个节点（形成环结构）
	if !found {
		// 遍历树获取第一个节点的键值对
		found = false
		ring.IteratorAsc(func(key, value any) bool {
			selectedNode = value.(*ServiceInfo)
			found = true
			return false // 找到第一个节点后停止遍历
		})
	}

	if found {
		// 返回对应的实际节点
		return selectedNode
	}

	glog.Warningf(context.Background(), "consistentHashSelector Select no node found, return random node")
	// 兜底方案：如果哈希环为空，随机返回一个节点
	return services[rand.Intn(len(services))]
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
