package gxyactor

import "gserver/core/gxyservice"

// ActorService 是 actor 类能力的服务注册基类。
// 它把本节点的 actor 协议地址作为服务地址注册,使其它节点能据以建立连接。
type ActorService struct {
	gxyservice.Service
}

// Host 返回本节点的 actor 协议地址(host:port)。
func (s *ActorService) Host() string {
	return address()
}
