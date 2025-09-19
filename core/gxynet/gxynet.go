package gxynet

import (
	"context"

	"gserver/core/gxymodule"
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/peer"

	"github.com/gogf/gf/v2/os/gcfg"
)

type Network struct {
	gxymodule.Module
	config  string
	peer    peer.Peer
	handler endpoint.EventHandler
}

func NewNetwork(config string, h endpoint.EventHandler) *Network {
	net := &Network{
		config:  config,
		handler: h,
	}
	return net
}

func (net *Network) OnInit(ctx context.Context) error {
	cfg := gcfg.Instance(net.config)
	peer, err := peer.NewPeer(cfg.MustGet(ctx, "gxynet.peer").String(), cfg)
	if err != nil {
		return err
	}
	net.peer = peer
	return nil
}

func (net *Network) OnStart(ctx context.Context) error {
	return net.peer.Start(ctx, net.handler)
}
