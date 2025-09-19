package gxynode

import (
	"context"
	"fmt"

	"gserver/core/gxyactor"
	"gserver/core/gxymodule"

	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/glog"
)

type node struct {
	rootModule gxymodule.Module
	AppID      string `toml:"app_id"`
	AppType    string `toml:"app_type"`
}

var n *node

func Node() *node {
	return n
}

func InitNode(config string) *node {
	n = &node{}
	cfg := gcfg.Instance(config)
	ctx := context.Background()
	n.AppID = cfg.MustGet(ctx, "app.app_id").String()
	n.AppType = cfg.MustGet(ctx, "app.app_type").String()
	n.LoadModule(gxyactor.NewActorSystem(gen.Atom(n.AppID), ""))
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

func (a *node) Node() string {
	if a == nil {
		return "default.0"
	}
	return fmt.Sprintf("%s.%s", a.AppType, a.AppID)
}

func (a *node) LoadModule(mod gxymodule.IModule) {
	if err := a.rootModule.AddModule(context.Background(), mod); err != nil {
		glog.Fatalf(context.Background(), "add module %v err: %s", mod.GetName(), err)
	}
}
