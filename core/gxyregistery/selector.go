package gxyregistery

import (
	"crypto/md5"
	"fmt"
	"math/rand"

	"github.com/gogf/gf/v2/container/gmap"
	"github.com/gogf/gf/v2/container/gtree"
)

type ServiceNode struct {
	Name   string
	Node   string
	Weight int
}

type ServiceSelector interface {
	Select(service string, nodes []ServiceNode) ServiceNode
}

type randomServiceSelector struct {
}

var randomSelector = &randomServiceSelector{}

func RandomSelector() ServiceSelector {
	return randomSelector
}

func (s *randomServiceSelector) Select(service string, nodes []ServiceNode) ServiceNode {
	if len(nodes) == 0 {
		return ServiceNode{}
	}
	return nodes[rand.Intn(len(nodes))]
}

type roundRobinServiceSelector struct {
	serviceIndex *gmap.StrIntMap
}

var roundRobinSelector = &roundRobinServiceSelector{
	serviceIndex: gmap.NewStrIntMap(true),
}

func RoundRobinSelector() ServiceSelector {
	return roundRobinSelector
}

func (s *roundRobinServiceSelector) Select(service string, nodes []ServiceNode) ServiceNode {
	if len(nodes) == 0 {
		return ServiceNode{}
	}
	var index int
	s.serviceIndex.LockFunc(func(m map[string]int) {
		index = m[service]
		m[service] = (index + 1) % len(nodes)
		index = m[service]
	})
	return nodes[index]
}

// consistentHashSelector 实现基于一致性哈希的服务选择器
// 一致性哈希算法可以在节点数量变化时最小化键的重新映射
// 通过虚拟节点机制提高哈希分布的均匀性

type consistentHashSelector struct {
	// 每个服务节点对应的一致性哈希环
	rings *gmap.StrAnyMap
	// 真实节点映射的虚拟节点数量
	virtualNodeCount int
}

var consistentHashSelectorInstance = &consistentHashSelector{
	rings:            gmap.NewStrAnyMap(true),
	virtualNodeCount: 10,
}

// ConsistentHashSelector 返回一致性哈希选择器实例
func ConsistentHashSelector() ServiceSelector {
	return consistentHashSelectorInstance
}

// ConsistentHashSelectorWithVirtualNodes 返回自定义虚拟节点数量的一致性哈希选择器
func ConsistentHashSelectorWithVirtualNodes() ServiceSelector {
	return &consistentHashSelector{
		rings: gmap.NewStrAnyMap(true),
	}
}

// Select 根据一致性哈希算法选择一个服务节点
// 使用service作为key计算哈希值，在哈希环上找到对应的节点
func (s *consistentHashSelector) Select(service string, nodes []ServiceNode) ServiceNode {
	if len(nodes) == 0 {
		return ServiceNode{}
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

	// 如果节点列表有变化，重建哈希环
	hashKey := fmt.Sprintf("%s:%d", service, len(nodes))
	hashNodesKey := s.getHashNodesKey(hashKey)
	existingNodesKey := s.rings.Get(hashNodesKey)

	if existingNodesKey == nil || existingNodesKey.(string) != hashKey {
		s.rebuildRing(ring, nodes, hashKey, hashNodesKey)
	}

	// 计算服务的哈希值
	hash := s.hash(service)

	// 在哈希环上查找大于等于当前哈希值的最小节点
	var selectedNode ServiceNode
	found := false

	// 遍历AVL树查找第一个大于等于当前哈希值的节点
	ring.IteratorAsc(func(key, value any) bool {
		currentHash := key.(uint32)
		if currentHash >= hash {
			selectedNode = value.(ServiceNode)
			found = true
			return false // 找到后停止遍历
		}
		return true // 继续遍历
	})

	// 如果没有找到，使用环上的第一个节点（形成环结构）
	if !found {
		firstKey := ring.Left()
		if firstKey != nil {
			selectedNode = ring.Get(firstKey).(ServiceNode)
			found = true
		}
	}

	if found {
		// 返回对应的实际节点
		return selectedNode
	}

	// 兜底方案：如果哈希环为空，随机返回一个节点
	return nodes[rand.Intn(len(nodes))]
}

// rebuildRing 重建一致性哈希环
func (s *consistentHashSelector) rebuildRing(ring *gtree.AVLTree, nodes []ServiceNode, hashKey, hashNodesKey string) {
	// 清空现有哈希环
	ring.Clear()

	// 为每个节点创建多个虚拟节点，并将它们添加到哈希环上
	for _, node := range nodes {
		for i := 0; i < s.virtualNodeCount; i++ {
			// 为虚拟节点生成唯一标识
			virtualNodeKey := fmt.Sprintf("%s:%s:%d", node.Name, node.Node, i)
			// 计算虚拟节点的哈希值
			hash := s.hash(virtualNodeKey)
			// 添加到哈希环
			ring.Set(hash, node)
		}
	}

	// 记录当前节点列表的标识，用于检测变化
	s.rings.Set(hashNodesKey, hashKey)
}

// hash 计算字符串的哈希值
func (s *consistentHashSelector) hash(key string) uint32 {
	// 使用MD5计算哈希值
	hash := md5.Sum([]byte(key))
	// 将16字节的MD5哈希转换为32位无符号整数
	return uint32(hash[0])<<24 | uint32(hash[1])<<16 | uint32(hash[2])<<8 | uint32(hash[3])
}

// getHashNodesKey 获取存储节点列表标识的键
func (s *consistentHashSelector) getHashNodesKey(ringKey string) string {
	return fmt.Sprintf("%s_nodes", ringKey)
}
