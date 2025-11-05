package world

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxyapp.go"
	"gserver/core/gxylog"
	"gserver/util"
	"reflect"

	"github.com/asynkron/protoactor-go/actor"
)

const (
	ACTIVITY_SERVER = "activity_server"
)

type worldApp struct {
	gxyapp.App
	servers []actor.Producer
}

func NewWorldApp() *worldApp {
	return &worldApp{
		servers: []actor.Producer{
			func() actor.Actor {
				return NewActivityServer()
			},
		},
	}
}

func (w *worldApp) OnModStart(ctx context.Context) error {
	for _, server := range w.servers {
		serverName := util.GetObjectName(server)
		gxyactor.ActorSystem().SpawnNamed(serverName, func() actor.Actor {
			return reflect.ValueOf(server).Call([]reflect.Value{})[0].Interface().(gxyactor.IActor)
		})
	}
	return nil
}

type ActivityServer struct {
	*gxyactor.ActorBase
}

func NewActivityServer() *ActivityServer {
	ctx := gxylog.NewContext(context.Background(), ACTIVITY_SERVER)
	return &ActivityServer{
		ActorBase: gxyactor.NewActorBase(ctx, nil),
	}
}

func (a *ActivityServer) HandleMessage(ctx context.Context, msg any) error {
	return nil
}
