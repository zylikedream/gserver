package gxynode

import (
	"context"

	"gserver/core/gxyactor"
	"gserver/core/gxymodule"

	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/glog"
)

type node struct {
	rootModule gxymodule.Module
	nodeName   string `toml:"node_name"`
}

var n *node

func Node() *node {
	return n
}

func InitNode(config string) *node {
	n = &node{}
	cfg := gcfg.Instance(config)
	ctx := context.Background()
	n.nodeName = cfg.MustGet(ctx, "node.node_name").String()
	n.LoadModule(gxyactor.NewActorSystem(gen.Atom(n.nodeName), ""))
	return n
}

func (a *node) Start(ctx context.Context) error {
	for _, mod := range a.rootModule.Modules() {
		// actorSys := gxyactor.ActorSystem().GetNode().ApplicationLoad()
		if err := mod.BaseModule().Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (a *node) Stop(ctx context.Context) error {
	for _, mod := range a.rootModule.Modules() {
		if err := mod.BaseModule().Stop(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (a *node) LoadModule(mod gxymodule.IModule) {
	if err := a.rootModule.AddModule(context.Background(), mod); err != nil {
		glog.Fatalf(context.Background(), "add module %v err: %+v", mod.GetName(), err)
	}
}
