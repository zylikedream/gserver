package main

import (
	"context"
	"os"

	"gserver/core/gxynode"

	"github.com/gogf/gf/v2/os/gcmd"
	"github.com/gogf/gf/v2/os/gproc"
)

func main() {
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
