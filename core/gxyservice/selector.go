package gxyservice

import (
	"math/rand"

	"github.com/gogf/gf/v2/container/gmap"
)

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
	index := s.serviceIndex.GetOrSet(service, 0)
	s.serviceIndex.Set(service, index+1)
	return nodes[index%len(nodes)]
}
