package gxyactor_test

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxymongo"
	"gserver/core/gxynode"
	"gserver/core/gxyredis"
	"gserver/service"
	"gserver/service/simple"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/gconv"
)

func Init() {
	node := gxynode.InitNode("node/game/config/game.toml")
	g.Cfg().GetAdapter().(*gcfg.AdapterFile).SetFileName("node/game/config/game.toml")
	node.LoadModule(gxyredis.NewRedisClient("node/config/db.toml"))
	node.LoadModule(gxymongo.NewMongoClient("node/config/db.toml"))
	node.LoadService(simple.SimpleService())
	node.Start(context.Background())
}

func BenchmarkGetGrain(b *testing.B) {
	Init()
	for i := 0; i < b.N; i++ {
		_, err := gxyactor.ActorSystem().GetGrain(service.SIMPLE_SERVICE, gconv.String(i))
		if err != nil {
			glog.Errorf(context.Background(), "err:%+v", err)
			b.Fatal(err)
		}
	}
}
