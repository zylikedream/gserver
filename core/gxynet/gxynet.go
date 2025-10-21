package gxynet

import (
	"context"

	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/peer"

	"github.com/gogf/gf/v2/os/gcfg"
)

type Network struct {
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

func (net *Network) Start(ctx context.Context) error {
	cfg := gcfg.Instance(net.config)
	peer, err := peer.NewPeer(cfg.MustGet(ctx, "gxynet.peer").String(), cfg)
	if err != nil {
		return err
	}
	net.peer = peer
	return net.peer.Start(ctx, net.handler)
}

func (net *Network) Stop(ctx context.Context) error {
	if net.peer != nil {
		if err := net.peer.Stop(ctx); err != nil {
			return err
		}
	}
	return nil
}
