package world

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxyapp.go"
	"gserver/util"
	"reflect"

	"github.com/asynkron/protoactor-go/actor"
)

type worldApp struct {
	gxyapp.App
	servers []gxyactor.IActor
}

func NewWorldApp() *worldApp {
	return &worldApp{
		servers: []gxyactor.IActor{
			&ActivityServer{},
		},
	}
}

func (w *worldApp) OnModInit(ctx context.Context) error {
	for _, server := range w.servers {
		serverName := util.GetObjectName(server)
		gxyactor.ActorSystem().SpawnNamed(serverName, func() actor.Actor {
			return util.NewObject(reflect.TypeOf(server)).(gxyactor.IActor)
		})
	}
	return nil
}

type ActivityServer struct {
	gxyactor.ActorBase
}

func (a *ActivityServer) HandleMessage(ctx context.Context, msg any) error {
	return nil
}
