package gxynode

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxyapp"
	"gserver/core/gxyhttp"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxymq"
	"gserver/core/gxypgx"
	"gserver/core/gxyredis"
	"gserver/core/gxyservice"
	"gserver/core/gxyutil"
	"gserver/src/apps/chat"
	"gserver/src/apps/friend"
	"gserver/src/apps/gateway"
	"gserver/src/apps/guild"
	"gserver/src/apps/role"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/glog"
)

type node struct {
	gxymodule.ModuleBase
	config           string
	Name             string
	NodeInstanceName string
	Host             string
	apps             []string
}

func NewNode(config string) *node {
	n := &node{
		config: config,
	}
	return n
}

func (n *node) OnModInit(ctx context.Context) error {
	gxyutil.SetConfig(n.config)
	cfg := g.Cfg()
	n.Name = cfg.MustGet(ctx, "node.name").String()
	n.Host = cfg.MustGet(ctx, "node.host").String()
	n.apps = cfg.MustGet(ctx, "node.apps").Strings()
	n.NodeInstanceName = fmt.Sprintf("%s@%x", n.Name, time.Now().UnixNano())
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
	deps := []string{"redis", "pgx", "actor", "service", "http"}
	for _, dep := range deps {
		if err := n.loadApp(ctx, dep, loaded); err != nil {
			return err
		}
	}
	for _, appName := range n.apps {
		if err := n.loadApp(ctx, appName, loaded); err != nil {
			return err
		}
	}
	return nil
}

// loadApp 递归加载 app 及其依赖，保证依赖先于使用者初始化
func (n *node) loadApp(ctx context.Context, appName string, loaded map[string]bool) error {
	if loaded[appName] {
		return nil
	}
	app := gxyapp.GetApp(appName)
	if app == nil {
		return gerror.Newf("app %s not found", appName)
	}

	loaded[appName] = true
	return n.AddModule(ctx, app)
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
	gxyapp.RegisterApp("redis", gxyredis.NewRedisApp())
	gxyapp.RegisterApp("pgx", gxypgx.NewPGXApp())
	gxyapp.RegisterApp("mq", gxymq.NewMessageQueueApp())
	gxyapp.RegisterApp("actor", gxyactor.NewActorApp(n.Name, n.NodeInstanceName, n.Host))
	gxyapp.RegisterApp("http", gxyhttp.NewHttpApp())
	gxyapp.RegisterApp("service", gxyservice.NewServiceApp(n.NodeInstanceName, n.Host))
	gxyapp.RegisterApp("chat", chat.NewChatApp())
	gxyapp.RegisterApp("friend", friend.NewFriendApp())
	gxyapp.RegisterApp("role", role.NewRoleApp())
	gxyapp.RegisterApp("gate", gateway.NewGateApp())
	gxyapp.RegisterApp("guild", guild.NewGuildApp())
}
