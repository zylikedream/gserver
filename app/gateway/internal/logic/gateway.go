package logic

import (
	"gserver/core/gxyactor"
	"gserver/core/gxyapp"
	"gserver/core/gxynet"

	// "gserver/core/gxyservice"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
)

func init() {
	app := gxyapp.InitApp("gate.toml")
	g.Cfg().GetAdapter().(*gcfg.AdapterFile).SetFileName("gate.toml")
	as := gxyactor.NewActorSystem("gate@127.0.0.1", "gate")
	app.LoadModule(gxynet.NewNetwork("gate.net.toml", NewGateHandler()))
	app.LoadModule(as)
}
