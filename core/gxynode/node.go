package gxynode

import (
	"context"
	"strings"

	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/glog"
)

type node struct {
	rootModule gxymodule.Module
	nodeName   string
	nodeType   string
	Host       string `v:"ipv4"`
}

var n *node

func Node() *node {
	return n
}

func InitNode(config string) *node {
	n = &node{}
	if err := n.init(config); err != nil {
		glog.Fatalf(context.Background(), "init node err: %+v", err)
		return nil
	}
	return n
}

func (n *node) init(config string) error {
	cfg := gcfg.Instance(config)
	ctx := context.Background()
	n.nodeName = cfg.MustGet(ctx, "node.node_name").String()
	if idx := strings.Index(n.nodeName, "@"); idx > 0 {
		n.Host = n.nodeName[idx+1:]
		n.nodeType = n.nodeName[:idx]
	}
	if n.Host == "" || n.nodeType == "" {
		return gerror.New("invalid nodename, should like 'node@ip'")
	}
	if err := g.Validator().Data(n).Run(ctx); err != nil {
		return gerror.Newf("validate node err: %+v", err)
	}
	logConfig, _ := cfg.Get(ctx, "log.config", "node/config/log.toml")
	if err := gxylog.InitLog(ctx, logConfig.String(), n.nodeType); err != nil {
		return gerror.Newf("init log err: %+v", err)
	}
	glog.Infof(context.Background(), "%s starting...", n.nodeType)
	svc := gxyactor.NewServiceManager()
	n.LoadModule(gxyactor.NewActorSystem(n.nodeName, n.Host))
	n.LoadModule(svc)
	return nil
}

func (a *node) Start(ctx context.Context) error {
	for _, mod := range a.rootModule.Modules() {
		// actorSys := gxyactor.ActorSystem().GetNode().ApplicationLoad()
		if err := mod.BaseModule().Start(ctx); err != nil {
			return err
		}
	}
	glog.Infof(context.Background(), "%s start success", n.nodeType)
	return nil
}

func (a *node) Stop(ctx context.Context) error {
	glog.Infof(context.Background(), "%s stopping...", n.nodeType)
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

func (a *node) LoadService(svc gxyactor.IService) {
	gxyactor.ServiceManager().LoadService(svc)
}
