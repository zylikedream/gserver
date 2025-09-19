package gxyapp

import (
	"context"
	"fmt"

	"gserver/core/gxyactor"
	"gserver/core/gxymodule"

	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/glog"
)

type Node struct {
	rootModule gxymodule.Module
	AppID      string `toml:"app_id"`
	AppType    string `toml:"app_type"`
}

var node *Node

func App() *Node {
	return node
}

func InitApp(config string) *Node {
	node = &Node{}
	cfg := gcfg.Instance(config)
	ctx := context.Background()
	node.AppID = cfg.MustGet(ctx, "app.app_id").String()
	node.AppType = cfg.MustGet(ctx, "app.app_type").String()
	node.LoadModule(gxyactor.NewActorSystem(gen.Atom(node.AppID), ""))
	return node
}

func (a *Node) Start(ctx context.Context) error {
	for _, mod := range a.rootModule.Modules() {
		// actorSys := gxyactor.ActorSystem().GetNode().ApplicationLoad()
		if err := mod.BaseModule().Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (a *Node) Stop(ctx context.Context) error {
	for _, mod := range a.rootModule.Modules() {
		if err := mod.BaseModule().Stop(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (a *Node) Node() string {
	if a == nil {
		return "default.0"
	}
	return fmt.Sprintf("%s.%s", a.AppType, a.AppID)
}

func (a *Node) LoadModule(mod gxymodule.IModule) {
	if err := a.rootModule.AddModule(context.Background(), mod); err != nil {
		glog.Fatalf(context.Background(), "add module %v err: %s", mod.GetName(), err)
	}
}
