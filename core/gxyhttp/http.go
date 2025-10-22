package gxyhttp

import (
	"gserver/core/gxyservice"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/gclient"
)

type HttpSystem struct {
	gxyservice.PublicService
	nodeName string
	host     string
}

func NewHttpSystem(nodeName string, host string) *HttpSystem {
	client := gclient.New()
	client.SetDiscovery(gxyservice.Discovery())
	client.Get()
	return &HttpSystem{
		nodeName: nodeName,
		host:     host,
	}
}
