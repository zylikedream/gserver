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
	"github.com/gogf/gf/v2/os/glog"
)

type grainInfo struct {
	Kind      string
	Props     *actor.Props
	Activator PID
}

type grainActivator struct {
	kind    string
	manager *grainManager
	childs  []PID
	timer   *ActorTimer
}

func (a *grainActivator) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		a.timer = NewActorTimer(ctx.Self())
		a.timer.AddTick(context.Background(), &Tick{
			Name: "update_register",
			Tick: 10 * time.Second,
		}, a.UpdateRegister)
	case *ActorTimerMsg:
		a.timer.Active(context.Background(), msg)
	case *pb.ActorActive:
		supervisor := newSupervisor()
		ginfo, ok := a.manager.grainInfos[a.kind]
		if !ok {
			ctx.Respond(&pb.ActorError{
				Reason: fmt.Sprintf("grain kind %s not registered", a.kind),
			})
			return
		}
		_, ok = a.manager.sys.system.ProcessRegistry.GetLocal(msg.Id)
		if ok {
			ctx.Respond(&remote.ActorPidResponse{
				Pid: actor.NewPID(a.manager.sys.Address(), msg.Id),
			})
			return
		}
		key := getGrainLocateKey(a.kind, msg.Id)
		err := a.manager.grainLocator.RegisterNode(context.Background(), key, a.manager.sys.Address(), 15*time.Second)
		if err != nil {
			glog.Error(context.Background(), "register grain node failed: %v", err)
			ctx.Respond(&pb.ActorError{
				Reason: err.Error(),
			})
			return
		}
		pid, err := a.manager.sys.system.Root.SpawnNamed(ginfo.Props.Configure(actor.WithSupervisor(supervisor)), msg.Id)
		if err != nil {
			glog.Error(context.Background(), "spawn grain actor failed: %s", err)
			ctx.Respond(&pb.ActorError{
				Reason: err.Error(),
			})
			return
		}
		ctx.Watch(pid)
		ctx.Respond(&remote.ActorPidResponse{
			Pid: pid,
		})
	case *actor.Terminated:
		child := msg.Who
		if child == nil {
			return
		}
		key := getGrainLocateKey(a.kind, child.Id)
		err := a.manager.grainLocator.UnregisterNode(context.Background(), key)
		if err != nil {
			glog.Error(context.Background(), "unregister grain node failed: %v", err)
		}
	}
}

func (a *grainActivator) UpdateRegister(ctx context.Context, _tm time.Time) {
	for _, child := range a.childs {
		key := getGrainLocateKey(a.kind, child.Id)
		err := a.manager.grainLocator.RegisterNode(context.Background(), key, a.manager.sys.Address(), 15*time.Second)
		if err != nil {
			glog.Error(context.Background(), "register grain node failed: %v", err)
		}
	}
}

type grainManager struct {
	sys          *actorSystem
	grainLocator *gxylocator.Locator
	registry     gxyregistery.IRegistery
	grainInfos   map[string]*grainInfo
}

func NewGrainManager(sys *actorSystem) *grainManager {
	return &grainManager{
		sys:        sys,
		grainInfos: make(map[string]*grainInfo),
	}
}

func (g *grainManager) Init(ctx context.Context) error {
	registry, err := gxyregistery.NewRegistery(gxyregistery.REGISTERY_TYPE_ETCD, "node/config/service.toml")
	if err != nil {
		return err
	}
	g.registry = registry
	g.grainLocator = gxylocator.NewLocator("gserver")
	return nil
}

func (g *grainManager) Stop(ctx context.Context) error {
	for _, ginfo := range g.grainInfos {
		g.registry.UnRegister(context.Background(), ginfo.Kind, g.sys.Address())
	}
	return nil
}

func (g *grainManager) grainReciveMiddleware(_ string) actor.ReceiverMiddleware {
	return func(next actor.ReceiverFunc) actor.ReceiverFunc {
		return func(ctx actor.ReceiverContext, envelope *actor.MessageEnvelope) {
			// key := getGrainLocateKey(kind, ctx.Value(actor.KeyId).(string))
			// err := g.grainLocator.RegisterNode(context.Background(), key, g.sys.Host())
			// if err != nil {
			// 	return nil, err
			// }
			next(ctx, envelope)
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
	pid, err := g.sys.system.Root.SpawnNamed(
		router.NewConsistentHashPool(5, actor.WithProducer(func() actor.Actor {
			return &grainActivator{kind: kind, manager: g}
		})), g.getManagerName(kind))
	if err != nil {
		return err
	}
	ginfo.Activator = pid
	g.grainInfos[kind] = ginfo
	if err := g.registry.Register(context.Background(), kind, g.sys.Address()); err != nil {
		return err
	}
	return nil
}

func (g *grainManager) spawnGrain(node string, kind string, id string) (PID, error) {
	activator := actor.NewPID(node, g.getManagerName(kind))
	rsp, err := g.sys.Call(activator, &pb.ActorActive{
		Kind: kind,
		Id:   id,
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
