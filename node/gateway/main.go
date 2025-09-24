package main

import (
	"context"
	"os"

	"gserver/core/gxynode"
	"gserver/service/gateway"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/gcmd"
	"github.com/gogf/gf/v2/os/gproc"
)

func Init() {
	node := gxynode.InitNode("config/gate.toml")
	g.Cfg().GetAdapter().(*gcfg.AdapterFile).SetFileName("gate.toml")
	node.LoadService(gateway.NewGateService())
	// node.LoadModule(gxynet.NewNetwork("config/gate.net.toml", gateway.NewGateHandler()))
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
		Brief: "start http server",
		Func: func(ctx context.Context, parser *gcmd.Parser) (err error) {
			return gxynode.Node().Start(ctx)
		},
	}
	gate.Run(ctx)
	gproc.AddSigHandlerShutdown(OnMainClose)
	// glog.Info(ctx, "main end")
	gproc.Listen()
}

func OnMainClose(s os.Signal) {
	ctx := context.Background()
	gxynode.Node().Stop(ctx)
}
