package gxynode

import (
	"context"
	"slices"

	"gserver/apps/gateway"
	"gserver/apps/role"
	"gserver/core/gxyactor"
	"gserver/core/gxyapp.go"
	"gserver/core/gxyhttp"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxymongo"
	"gserver/core/gxymq"
	"gserver/core/gxyredis"
	"gserver/core/gxyservice"
	"gserver/util"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/glog"
)

type node struct {
	gxymodule.ModuleBase
	config string
	Name   string
	Host   string
	apps   []string
}

func NewNode(config string) *node {
	n := &node{
		config: config,
	}
	return n
}

func (n *node) OnModInit(ctx context.Context) error {
	util.SetConfig(n.config)
	cfg := g.Cfg()
	n.Name = cfg.MustGet(ctx, "node.name").String()
	n.Host = cfg.MustGet(ctx, "node.host").String()
	n.apps = cfg.MustGet(ctx, "node.apps").Strings()
	if n.Host == "" || n.Name == "" {
		return gerror.New("no name or host'")
	}
	if err := g.Validator().Data(n).Run(ctx); err != nil {
		return gerror.Newf("validate node err: %+v, check host is ipv4?", err)
	}
	if err := gxylog.InitLog(ctx, n.Name); err != nil {
		return gerror.Newf("init log err: %+v", err)
	}
	n.registerApps()

	return nil
}

func (a *node) GetModName() string {
	return a.Name
}

func (n *node) OnModStart(ctx context.Context) error {
	glog.Infof(context.Background(), "node %s starting....", n.Name)
	loaded := map[string]bool{}
	preloaded := []gxyapp.IApp{
		gxyactor.NewActorApp(n.Name, n.Host),
		gxyhttp.NewHttpApp(n.Name, n.Host),
		gxyservice.NewServiceApp(n.Name),
	}
	needed := []gxyapp.IApp{}
	for _, appName := range n.apps {
		app := gxyapp.GetApp(appName)
		if app == nil {
			return gerror.Newf("app %s not found", appName)
		}
		loaded[appName] = true
		needed = append(needed, app)
	}
	for _, app := range slices.Concat(preloaded, needed) {
		if err := n.AddModule(ctx, app); err != nil {
			return gerror.Newf("add app %s err: %+v", app.AppName(), err)
		}
	}
	return nil
}

func (n *node) OnModStartAfter(ctx context.Context) error {
	glog.Infof(context.Background(), "node %s start success", n.Name)
	return nil
}

func (n *node) OnModStopBefore(ctx context.Context) error {
	glog.Infof(context.Background(), "node %s stopping...", n.Name)
	return nil
}

func (n *node) OnModStop(ctx context.Context) error {
	glog.Infof(context.Background(), "node %s stop success", n.Name)
	return nil
}

func (n *node) registerApps() {

	gxyapp.RegisterApp("role", role.NewRoleApp())
	gxyapp.RegisterApp("redis", gxyredis.NewRedisApp())
	gxyapp.RegisterApp("mongo", gxymongo.NewMongoApp())
	gxyapp.RegisterApp("mq", gxymq.NewMessageQueueApp())
	gxyapp.RegisterApp("service", gxyservice.NewServiceApp(n.Name))
	gxyapp.RegisterApp("gate", gateway.NewGateApp())
}
