package main

import (
	"context"
	"os"

	"gserver/core/gxymongo"
	"gserver/core/gxynode"
	"gserver/core/gxyredis"
	"gserver/service/gateway"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/gcmd"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/os/gproc"
)

func Init() {
	node := gxynode.InitNode("config/gate.toml")
	g.Cfg().GetAdapter().(*gcfg.AdapterFile).SetFileName("gate.toml")
	node.LoadModule(gxyredis.NewRedisClient("node/config/db.toml"))
	node.LoadModule(gxymongo.NewMongoClient("node/config/db.toml"))
	node.LoadService(gateway.GateService())
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
			if err = gxynode.Node().Start(ctx); err != nil {
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
	gxynode.Node().Stop(ctx)
}
