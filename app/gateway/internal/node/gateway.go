package node

import (
	"gserver/app/gateway/internal/logic"
	"gserver/core/gxynet"
	"gserver/core/gxynode"

	// "gserver/core/gxyservice"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
)

func init() {
	node := gxynode.InitNode("gate.toml")
	g.Cfg().GetAdapter().(*gcfg.AdapterFile).SetFileName("gate.toml")
	node.LoadModule(gxynet.NewNetwork("gate.net.toml", logic.NewGateHandler()))
}
