package ergoapp

import (
	"context"
	"time"

	"ergo.services/ergo/gen"
	"github.com/cockroachdb/errors"
	"github.com/gogf/gf/v2/frame/g"
	"gserver/core/gxyactor"
	ergo "gserver/core/gxyactor/internal/ergo"
	"gserver/core/gxyapp"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
)

type actorApp struct {
	gxyapp.App
	nodeName, nodeInstanceName, host string
	adapter                          *ergo.Adapter
	manager                          interface {
		gxymodule.IModule
		ActivateActor(context.Context, string, string, bool) (gxyactor.PID, error)
		GetActorOwner(context.Context, string, string) (gxyactor.ActorOwner, error)
		RegisterActorKind(string, gxyactor.ActorProducer) error
	}
}

func NewActorApp(nodeName, nodeInstanceName, host string) *actorApp {
	a := &actorApp{nodeName: nodeName, nodeInstanceName: nodeInstanceName, host: host}
	return a
}

func (a *actorApp) OnModInit(ctx context.Context) error {
	port := g.Cfg().MustGet(ctx, "port.actor").Int()
	manager := gxyactor.NewActivatorManager(a.nodeName, a.nodeInstanceName)
	a.manager = manager
	cookie := g.Cfg().MustGet(ctx, "actor.cookie").String()
	adapter, err := ergo.Start(ergo.Options{
		NodeName:         a.nodeInstanceName,
		NodeInstanceName: a.nodeInstanceName,
		Host:             a.host,
		Port:             port,
		Cookie:           cookie,
		ShutdownTimeout:  15 * time.Second,
		Network:          gen.NetworkOptions{},
		Activation:       manager.ActivateActor,
		RegisterActorKind: func(kind string, producer gxyactor.ActorProducer) error {
			return manager.RegisterActorKind(kind, producer)
		},
	})
	if err != nil {
		return err
	}
	a.adapter = adapter
	return a.AddModule(ctx, manager)
}

func (a *actorApp) OnModStart(ctx context.Context) error {
	gxylog.Info(ctx, "ergo actor started", gxylog.Str("node", a.nodeInstanceName), gxylog.Str("address", a.Address()))
	return nil
}
func (a *actorApp) OnModStop(ctx context.Context) error {
	if a.adapter != nil {
		a.adapter.StopNode(15 * time.Second)
	}
	return nil
}
func (a *actorApp) RegisterActorKind(kind string, producer gxyactor.ActorProducer) error {
	if a.adapter == nil {
		return errors.New("actor app is not initialized")
	}
	return a.adapter.RegisterActorKind(kind, producer)
}
func (a *actorApp) DeregisterActorKind(kind string) {
	if a.adapter != nil {
		a.adapter.DeregisterActorKind(kind)
	}
}
func (a *actorApp) Address() string {
	if a.adapter == nil {
		return ""
	}
	return a.adapter.Address()
}
