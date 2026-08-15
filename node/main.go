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
			// 不用 gcmd 的 ctx:gcmd.doRun 会创建根 span(挂 goframe 默认 provider,
			// 无导出器,永不导出到 Tempo),继承它会让启动日志带上孤儿 trace_id。
			if err = rootModule.StartModule(context.Background()); err != nil {
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
	_ = rootModule.StopModule(ctx)
}
