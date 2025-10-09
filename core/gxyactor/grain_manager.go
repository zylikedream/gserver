package gxyactor

import (
	"context"
	"fmt"
	"gserver/core/gxylocator"
	"gserver/core/gxyregistery"
	"gserver/protocol/pb"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/asynkron/protoactor-go/remote"
	"github.com/asynkron/protoactor-go/router"
	"github.com/gogf/gf/v2/errors/gerror"
)

type grainInfo struct {
	Kind      string
	Props     *actor.Props
	Activator PID
}

type grainActivator struct {
	kind    string
	manager *grainManager
}

func (a *grainActivator) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *remote.ActorPidRequest:
		ginfo, ok := a.manager.grainInfos[msg.Kind]
		if !ok {
			ctx.Respond(&pb.ActorError{
				Reason: fmt.Sprintf("grain kind %s not registered", msg.Kind),
			})
			return
		}
		_, ok = a.manager.sys.system.ProcessRegistry.GetLocal(msg.Name)
		if ok {
			ctx.Respond(&remote.ActorPidResponse{
				Pid: actor.NewPID(a.manager.sys.nodeName, msg.Name),
			})
			return
		}
		pid, err := a.manager.sys.system.Root.SpawnNamed(ginfo.Props, msg.Name)
		if err != nil {
			ctx.Respond(&pb.ActorError{
				Reason: err.Error(),
			})
			return
		}
		ctx.Respond(&remote.ActorPidResponse{
			Pid: pid,
		})
	}
}

type grainManager struct {
	sys          *actorSystem
	grainLocator *gxylocator.Locator
	registry     gxyregistery.IRegistery
	grainInfos   map[string]*grainInfo
}

func NewGrainManager(sys *actorSystem) *grainManager {
	registry, err := gxyregistery.NewRegistery(gxyregistery.REGISTERY_TYPE_ETCD, "../config/service.toml")
	if err != nil {
		panic(err)
	}
	return &grainManager{
		sys:          sys,
		grainLocator: gxylocator.NewLocator("gserver", 30*time.Second),
		registry:     registry,
		grainInfos:   make(map[string]*grainInfo),
	}
}

func (g *grainManager) grainReciveMiddleware(kind string) actor.ReceiverMiddleware {
	return func(next actor.ReceiverFunc) actor.ReceiverFunc {
		return func(ctx actor.ReceiverContext, envelope *actor.MessageEnvelope) {
			// key := getGrainLocateKey(kind, ctx.Value(actor.KeyId).(string))
			// err := g.grainLocator.RegisterNode(context.Background(), key, g.sys.Host())
			// if err != nil {
			// 	return nil, err
			// }
			// return next(ctx, message)
		}
	}
}

func (g *grainManager) getManagerName(kind string) string {
	return fmt.Sprintf("%s_%s", "GrainManager", kind)
}

func (g *grainManager) RegisterGrain(kind string, props actor.Producer) error {
	newProps := actor.PropsFromProducer(props,
		actor.WithReceiverMiddleware(g.grainReciveMiddleware(kind)))
	ginfo := &grainInfo{
		Kind:  kind,
		Props: newProps,
	}
	// g.sys.remote.Register(kind, newProps)
	pid, err := g.sys.system.Root.SpawnNamed(
		router.NewConsistentHashPool(5, actor.WithProducer(func() actor.Actor {
			return &grainActivator{kind: kind, manager: g}
		})), g.getManagerName(kind))
	if err != nil {
		return err
	}
	ginfo.Activator = pid
	g.grainInfos[kind] = ginfo
	return nil
}

func (g *grainManager) spawnGrain(node string, kind string, id string) (PID, error) {
	activator := actor.NewPID(node, g.getManagerName(kind))
	rsp, err := g.sys.Call(activator, &remote.ActorPidRequest{
		Kind: kind,
		Name: id,
	})
	if err != nil {
		return nil, err
	}
	return rsp.(*remote.ActorPidResponse).Pid, nil
}

func (g *grainManager) getGrain(kind string, id string, spawn bool) (PID, error) {
	ctx := context.Background()
	key := getGrainLocateKey(kind, id)
	node, err := g.grainLocator.LocateNode(ctx, key)
	if err != nil {
		return nil, err
	}
	if node != "" {
		return actor.NewPID(node, id), nil
	}
	if !spawn {
		return nil, gerror.Newf("grain %s:%s not found", kind, id)
	}

	grainNode := g.getKindNode(kind, gxyregistery.RoundRobinSelector())

	if grainNode.Node == "" {
		return nil, gerror.Newf("find grain node failed, kind: %s, id: %s", kind, id)
	}
	return g.spawnGrain(grainNode.Node, kind, id)
}

func (s *grainManager) getKindNode(kind string, selector gxyregistery.ServiceSelector) gxyregistery.ServiceNode {
	nodes, err := s.registry.Search(context.Background(), kind)
	if err != nil {
		return gxyregistery.ServiceNode{}
	}

	return selector.Select(kind, nodes)
}

func getGrainLocateKey(kind string, id string) string {
	return fmt.Sprintf("%s_%s", kind, id)
}
