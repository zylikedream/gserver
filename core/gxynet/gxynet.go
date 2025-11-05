package gxynet

import (
	"context"

	"gserver/core/gxymodule"
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/peer"

	"github.com/gogf/gf/v2/os/gcfg"
)

type Network struct {
	gxymodule.ModuleBase
	cfg     *gcfg.Config
	peer    peer.Peer
	handler endpoint.EventHandler
}

func NewNetwork(cfg *gcfg.Config, h endpoint.EventHandler) *Network {
	net := &Network{
		cfg:     cfg,
		handler: h,
	}
	return net
}

func (net *Network) OnModStart(ctx context.Context) error {
	cfg := net.cfg
	peer, err := peer.NewPeer(cfg.MustGet(ctx, "gxynet.peer").String(), cfg)
	if err != nil {
		return err
	}
	net.peer = peer
	return net.peer.Start(ctx, net.handler)
}

func (net *Network) OnModStop(ctx context.Context) error {
	if net.peer != nil {
		if err := net.peer.Stop(ctx); err != nil {
			return err
		}
	}
	return nil
}
