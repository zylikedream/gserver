package gxynode

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxyapp"
	"gserver/core/gxyhttp"
	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxymodule"
	"gserver/core/gxymq"
	"gserver/core/gxypgx"
	"gserver/core/gxyredis"
	"gserver/core/gxyservice"
	"gserver/core/gxytrace"
	"gserver/core/gxyutil"
	"gserver/src/apps/chat"
	"gserver/src/apps/friend"
	"gserver/src/apps/gateway"
	"gserver/src/apps/guild"
	"gserver/src/apps/role"
	"gserver/src/apps/thanks"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

type node struct {
	gxymodule.ModuleBase
	config           string
	Name             string
	NodeInstanceName string
	apps             []string
	Host             string
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
	n.apps = cfg.MustGet(ctx, "node.apps").Strings()
	n.Host = cfg.MustGet(ctx, "node.host").String()
	if ip := os.Getenv("POD_IP"); ip != "" {
		n.Host = ip
	}
	podName := n.Name
	if h := os.Getenv("HOSTNAME"); h != "" && strings.HasPrefix(h, n.Name+"-") {
		podName = h
	}
	n.NodeInstanceName = fmt.Sprintf("%s@%x", podName, time.Now().UnixNano())
	if n.Name == "" {
		return gerror.New("no node name '")
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
	gxylog.Info(context.Background(), "node starting....", gxylog.Str("name", n.Name))

	loaded := map[string]bool{}
	deps := []string{"metrics", "trace", "redis", "pgx", "actor", "service", "http", "mq"}
	for _, dep := range deps {
		if err := n.loadApp(ctx, dep, loaded); err != nil {
			return err
		}
		gxylog.Info(context.Background(), "app loaded success", gxylog.Str("app", dep))
	}
	for _, appName := range n.apps {
		if err := n.loadApp(ctx, appName, loaded); err != nil {
			return err
		}
		gxylog.Info(context.Background(), "app loaded success", gxylog.Str("app", appName))
	}
	n.loadApp(ctx, "thanks", loaded)
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
	if err := n.AddModule(ctx, app); err != nil {
		return err
	}
	return nil
}

func (n *node) OnModStartAfter(ctx context.Context) error {
	gxylog.Info(context.Background(), "node start success", gxylog.Str("name", n.Name))
	return nil
}

func (n *node) OnModStopBefore(ctx context.Context) error {
	gxylog.Info(context.Background(), "node stopping...", gxylog.Str("name", n.Name))
	return nil
}

func (n *node) OnModStop(ctx context.Context) error {
	gxylog.Info(context.Background(), "node stop success", gxylog.Str("name", n.Name))
	return nil
}

func (n *node) registerApps() {
	gxyapp.RegisterApp("metrics", gxymetrics.NewMetricsApp())
	gxyapp.RegisterApp("trace", gxytrace.NewTraceApp())
	gxyapp.RegisterApp("redis", gxyredis.NewRedisApp())
	gxyapp.RegisterApp("pgx", gxypgx.NewPGXApp())
	gxyapp.RegisterApp("mq", gxymq.NewMessageQueueApp())
	gxyapp.RegisterApp("actor", gxyactor.NewActorApp(n.Name, n.NodeInstanceName, n.Host))
	gxyapp.RegisterApp("http", gxyhttp.NewHttpApp())
	gxyapp.RegisterApp("service", gxyservice.NewServiceApp(n.NodeInstanceName))
	gxyapp.RegisterApp("chat", chat.NewChatApp(n.Host))
	gxyapp.RegisterApp("friend", friend.NewFriendApp(n.Host))
	gxyapp.RegisterApp("role", role.NewRoleApp())
	gxyapp.RegisterApp("gate", gateway.NewGateApp())
	gxyapp.RegisterApp("thanks", thanks.NewThanksApp())
	gxyapp.RegisterApp("guild", guild.NewGuildApp(n.Host))
}
