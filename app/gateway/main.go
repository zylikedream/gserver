package main

import (
	"context"
	"os"

	"gserver/core/gxyapp"

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
			return gxyapp.App().Start(ctx)
		},
	}
	gate.Run(ctx)
	gproc.AddSigHandlerShutdown(OnMainClose)
	// glog.Info(ctx, "main end")
	gproc.Listen()
}

func OnMainClose(s os.Signal) {
	ctx := context.Background()
	gxyapp.App().Stop(ctx)
}
