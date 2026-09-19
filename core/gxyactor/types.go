package gxyactor

import "gserver/core/gxyservice"

// ActorNodeServiceName 是节点级服务记录的固定名。
//
// 运行时只按**节点名**解析连接地址,而能力记录回答不了"某节点的 actor 地址
// 是什么":一个节点可以承载多种能力(开发与回归用的集中配置即是如此),
// 也可以一种都不承载(如纯网关节点)。因此每个节点额外登记一条以本名索引的
// 记录,地址是其 actor 协议地址。
const ActorNodeServiceName = "actor-node"

// ActorNodeService 是节点级服务:名字固定,地址是本节点的 actor 协议地址。
//
// 所有节点都会登记它(actor 组件是节点固定加载的),因此任何节点名都能被
// 解析到可达地址,与它承载哪些能力无关。
type ActorNodeService struct {
	gxyservice.Service
}

// ServiceName 返回固定的节点级服务名。
func (s *ActorNodeService) ServiceName() string {
	return ActorNodeServiceName
}

// Host 返回本节点的 actor 协议地址(host:port)。
func (s *ActorNodeService) Host() string {
	return address()
}

// ActorService 是 actor 类能力的服务注册基类。
// 它把本节点的 actor 协议地址作为服务地址注册,使其它节点能据以建立连接。
type ActorService struct {
	gxyservice.Service
}

// Host 返回本节点的 actor 协议地址(host:port)。
func (s *ActorService) Host() string {
	return address()
}
