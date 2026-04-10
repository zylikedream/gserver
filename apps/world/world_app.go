package world

import (
	"context"
	"gserver/apps/world/server"
	"gserver/core/gxyactor"
	"gserver/core/gxyapp.go"

	"github.com/asynkron/protoactor-go/actor"
)

type Server struct {
	Name     string
	Producer actor.Producer
}

type worldApp struct {
	gxyapp.App
	servers []Server
}

func NewWorldApp() *worldApp {
	return &worldApp{
		servers: []Server{
			{
				Name: server.ACTIVITY_SERVER,
				Producer: func() actor.Actor {
					return server.NewActivityServer()
				},
			},
		},
	}
}

func (w *worldApp) OnModStart(ctx context.Context) error {
	for _, server := range w.servers {
		gxyactor.SpawnNamedFunc(server.Name, server.Producer)
	}
	return nil
}
