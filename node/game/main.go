package main

import (
	"context"
	"os"

	"gserver/core/gxymodule"
	"gserver/core/gxynode"
	"gserver/service/role"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/gcmd"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/os/gproc"
)

var rootModule gxymodule.ModuleBase

func Init() {
	node := gxynode.InitNode("config/game.toml")
	g.Cfg().GetAdapter().(*gcfg.AdapterFile).SetFileName("config/game.toml")
	rootModule.AddModule(context.Background(), node)
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
		Brief: "game server",
		Func: func(ctx context.Context, parser *gcmd.Parser) (err error) {
			if err = rootModule.StartModule(ctx); err != nil {
				glog.Fatalf(ctx, "start game failed: %+v", err)
			}
			return nil
		},
	}
	gate.Run(ctx)
	gproc.AddSigHandlerShutdown(OnMainClose)
	// glog.Info(ctx, "main end")
	gproc.Listen()
}

func OnMainClose(s os.Signal) {
	ctx := context.Background()
	rootModule.StopModule(ctx)
}
