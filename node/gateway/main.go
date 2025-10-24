package main

import (
	"context"
	"os"

	"gserver/core/gxymodule"
	"gserver/core/gxymongo"
	"gserver/core/gxynode"
	"gserver/service/friend"
	"gserver/service/gateway"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/gcmd"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/os/gproc"
)

var rootModule gxymodule.ModuleBase

func Init() {
	node := gxynode.InitNode("config/gate.toml")
	g.Cfg().GetAdapter().(*gcfg.AdapterFile).SetFileName("gate.toml")
	node.LoadModule(gxymongo.NewMongoClient("node/config/db.toml"))
	node.LoadService(gateway.GateService())
	rootModule.AddModule(context.Background(), node)
	node.LoadService(friend.FriendService())
}

func main() {
	Init()
	run()
}

func run() {
	ctx := context.Background()
	gate := gcmd.Command{
		Name:  "main",
		Usage: "main",
		Brief: "gate server",
		Func: func(ctx context.Context, parser *gcmd.Parser) (err error) {
			if err = rootModule.StartModule(ctx); err != nil {
				glog.Fatalf(ctx, "start gate failed: %+v", err)
			}
			return nil
		},
	}
	gate.Run(ctx)
	gproc.AddSigHandlerShutdown(OnMainClose)
	gproc.Listen()
}

func OnMainClose(s os.Signal) {
	ctx := context.Background()
	rootModule.StopModule(ctx)
}
