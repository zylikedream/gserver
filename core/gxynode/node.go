package gxynode

import (
	"context"

	"gserver/core/gxyapp.go"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/glog"
)

type node struct {
	gxymodule.ModuleBase
	Name string `toml:"name"`
	Host string `toml:"host" v:"ipv4"`
	apps []string
}

func InitNode(config string) *node {
	n := &node{}
	if err := n.init(config); err != nil {
		glog.Fatalf(context.Background(), "init node err: %+v", err)
		return nil
	}
	return n
}

func (n *node) init(config string) error {
	g.Cfg().GetAdapter().(*gcfg.AdapterFile).SetFileName(config)
	cfg := g.Cfg()
	ctx := context.Background()
	n.Name = cfg.MustGet(ctx, "node.name").String()
	n.Host = cfg.MustGet(ctx, "node.host").String()
	n.apps = cfg.MustGet(ctx, "node.apps").Strings()
	if n.Host == "" || n.Name == "" {
		return gerror.New("no name or host'")
	}
	if err := g.Validator().Data(n).Run(ctx); err != nil {
		return gerror.Newf("validate node err: %+v, check host is ipv4?", err)
	}
	logConfig, _ := cfg.Get(ctx, "log.config", "node/config/log.toml")
	if err := gxylog.InitLog(ctx, logConfig.String(), n.Name); err != nil {
		return gerror.Newf("init log err: %+v", err)
	}

	return nil
}

func (a *node) GetModName() string {
	return a.Name
}

func (n *node) OnModInit(ctx context.Context) error {
	for _, appName := range n.apps {
		app := gxyapp.GetApp(appName)
		if err := n.AddModule(ctx, app); err != nil {
			return gerror.Newf("add module %s err: %+v", appName, err)
		}
	}
	return nil
}

func (a *node) OnModStart(ctx context.Context) error {
	glog.Infof(context.Background(), "node %s starting....", a.Name)
	return nil
}

func (a *node) OnModStartAfter(ctx context.Context) error {
	glog.Infof(context.Background(), "node %s start success", a.Name)
	return nil
}

func (a *node) OnModStopBefore(ctx context.Context) error {
	glog.Infof(context.Background(), "node %s stopping...", a.Name)
	return nil
}

func (a *node) OnModStop(ctx context.Context) error {
	glog.Infof(context.Background(), "node %s stop success", a.Name)
	return nil
}
