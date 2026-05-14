package main

import (
	"context"
	"os"

	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxynode"

	"github.com/gogf/gf/v2/os/gcmd"
	"github.com/gogf/gf/v2/os/gproc"
	"github.com/gogf/gf/v2/text/gstr"
)

var rootModule gxymodule.ModuleBase

func Init() {
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
			config := parser.GetOpt("config", "").String()
			node := gxynode.NewNode(gstr.Trim(config))
			if err = rootModule.AddModule(context.Background(), node); err != nil {
				gxylog.Fatal(ctx, "init node failed", gxylog.Err(err))
			}
			if err = rootModule.StartModule(ctx); err != nil {
				gxylog.Fatal(ctx, "start game failed", gxylog.Err(err))
			}
			if _, ok := parser.GetOptAll()["pressure"]; ok {
				gxylog.SetLevel("info")
				gxylog.Info(ctx, "pressure mode enabled, log level set to info")
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
