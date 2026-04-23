package gxyactor

import (
	"context"
	"fmt"
	"gserver/core/gxylocator"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxyregistery"
	"gserver/core/gxyservice"
	"gserver/core/gxytimer"
	"gserver/protocol/pb"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/asynkron/protoactor-go/remote"
	"github.com/asynkron/protoactor-go/router"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	ActorLocateTTL            = 40 * time.Second
	ActorLocateUpdateInterval = 30 * time.Second
)

type activatorMeta struct {
	Kind      string
	Props     *actor.Props
	Activator PID
	mgr       *ActorMgr
}

type actorActivator struct {
	*ActorBase
	kind    string
	manager *activatorManager
	childs  map[PID]string
	meta    *activatorMeta
}

func NewActorActivator(kind string, manager *activatorManager) *actorActivator {
	a := &actorActivator{
		kind:    kind,
		manager: manager,
		childs:  make(map[PID]string),
	}
	ctx := gxylog.NewContext(context.Background(), "actor_activator")
	a.ActorBase = NewActorBase(ctx, a)
	return a
}

func (a *actorActivator) DelayInit(ctx context.Context) error {
	info, ok := a.manager.activatorMetas[a.kind]
	if !ok {
		return fmt.Errorf("actor kind %s not registered", a.kind)
	}
	a.meta = info
	a.Timer().AddTick(a.ctx, &gxytimer.Tick{
		Name:     "locate_tick",
		Interval: ActorLocateUpdateInterval,
	}, func(ctx context.Context, _ gxytimer.TimerActiveInfo) {
		a.renewAllActors(ctx)
	})
	return nil
}

func (a *actorActivator) HandleMessage(ctx context.Context, msg any) error {
	switch msg := msg.(type) {
	case *pb.ActorActive:
		props := a.meta.Props.Clone()
		pid, err := SpawnNamed(props, msg.Id, msg.Id)
		if err != nil {
			if err == actor.ErrNameExists {
				a.Respond(&remote.ActorPidResponse{Pid: pid})
				return nil
			}
			a.Respond(ActorError(err.Error()))
			return nil
		}

		// 立即注册 Redis（SETNX 语义）
		key := getActorLocateKey(a.kind, msg.Id)
		if err := a.registerActor(a.ctx, key, pid); err != nil {
			// key 已被抢，停止本地 actor
			StopActor(pid)
			a.Respond(&pb.ActorError{Reason: "registration failed, key taken by another node"})
			return nil
		}

		// 注册成功，加入 childs
		a.childs[pid] = msg.Id
		a.meta.mgr.Add(msg.Id, pid)

		// Touch 确认（异步，不影响注册流程）
		sender := a.Actx.Sender()
		go func() {
			_, _ = a.Call(pid, &actor.Touch{}, 2*time.Second)
			a.Send(sender, &remote.ActorPidResponse{Pid: pid})
		}()

		return nil

	// 父actor spawn出来的子actor在terminate后会，给父actor发送Terminate消息
	case *actor.Terminated:
		child := msg.Who
		if child == nil {
			return nil
		}
		a.Actx.Children()
		id := a.childs[child]
		if id == "" {
			return nil
		}
		key := getActorLocateKey(a.kind, id)
		// 立即删除 Redis key，不等 TTL 过期
		if err := a.deRegisterActor(a.ctx, key, child); err != nil {
			glog.Errorf(a.ctx, "deregister actor node %s failed: %v", key, err)
		}
		a.meta.mgr.Remove(id)
		delete(a.childs, child)
		return nil
	}
	return nil
}

func (a *actorActivator) Terminate(ctx context.Context, err error) {
	glog.Info(ctx, "actor activator stopped")
}

func (a *actorActivator) registerActor(ctx context.Context, key string, pid PID) error {
	pidInfo, _ := protojson.Marshal(&pb.ActorPid{
		Address: pid.Address,
		Id:      pid.Id,
	})
	return a.manager.locator.MustRegisterActor(ctx, key, string(pidInfo), ActorLocateTTL)
}

func (a *actorActivator) deRegisterActor(ctx context.Context, key string, pid PID) error {
	pidInfo, _ := protojson.Marshal(&pb.ActorPid{
		Address: pid.Address,
		Id:      pid.Id,
	})
	return a.manager.locator.UnregisterActor(ctx, key, string(pidInfo))
}

type activatorManager struct {
	gxymodule.ModuleBase
	locator        *gxylocator.Locator
	activatorMetas map[string]*activatorMeta
	ctx            context.Context
}

func NewActivatorManager() *activatorManager {
	return &activatorManager{
		activatorMetas: make(map[string]*activatorMeta),
		ctx:            gxylog.NewContext(context.Background(), "activatorManager"),
	}
}

func (g *activatorManager) OnModInit(ctx context.Context) error {
	g.locator = gxylocator.NewLocator("gserver")
	return nil
}

func (g *activatorManager) OnModStop(ctx context.Context) error {
	for _, info := range g.activatorMetas {
		StopActor(info.Activator)
	}
	return nil
}

func (g *activatorManager) getPoolName(kind string) string {
	return fmt.Sprintf("%s_%s", "ActivatorPool", kind)
}

func (g *activatorManager) RegisterActorKind(kind string, prod ActorProducer) error {
	actorProps := actor.PropsFromProducer(func() actor.Actor {
		return prod()
	}, actor.WithSupervisor(newSupervisor()))
	meta := &activatorMeta{
		Kind:  kind,
		Props: actorProps,
	}
	g.activatorMetas[kind] = meta
	pid, err := SpawnNamed(
		router.NewConsistentHashPool(5, actor.WithProducer(func() actor.Actor {
			return NewActorActivator(kind, g)
		})), g.getPoolName(kind))
	if err != nil {
		g.activatorMetas[kind] = nil
		return err
	}
	meta.Activator = pid
	meta.mgr = NewActorMgr(fmt.Sprintf("%s_%s", "actorMgr", kind))
	return nil
}

func (g *activatorManager) DeregisterActorKind(kind string) {
	info, ok := g.activatorMetas[kind]
	if !ok {
		return
	}
	StopActor(info.Activator)
	delete(g.activatorMetas, kind)
}

func (g *activatorManager) spawnActor(node string, kind string, id string) (PID, error) {
	glog.Debugf(g.ctx, "spawn actor %s:%s at %s", kind, id, node)
	activator := actor.NewPID(node, g.getPoolName(kind))
	rsp, err := Call(activator, &pb.ActorActive{
		Kind: kind,
		Id:   id,
	}, -1)
	if err != nil {
		return nil, err
	}
	if rsp, ok := rsp.(*pb.ActorError); ok {
		return nil, gerror.New(rsp.Reason)
	}
	return rsp.(*remote.ActorPidResponse).Pid, nil
}

func (g *activatorManager) getActor(kind string, id string, spawn bool) (PID, error) {
	ctx := context.Background()
	key := getActorLocateKey(kind, id)
	pidInfo, err := g.locator.LocateNode(ctx, key)
	if err != nil {
		return nil, err
	}
	if pidInfo != "" {
		var pid pb.ActorPid
		err := protojson.Unmarshal([]byte(pidInfo), &pid)
		if err != nil {
			return nil, err
		}
		return actor.NewPID(pid.Address, pid.Id), nil
	}
	if !spawn {
		return nil, nil
	}

	serviceInfo := gxyservice.ServiceApp().GetServiceInfo(ctx, kind, key, gxyregistery.ConsistentHashSelector())

	if serviceInfo == nil || serviceInfo.NodeHost == "" {
		return nil, gerror.Newf("find actor node failed, kind: %s, id: %s", kind, id)
	}
	return g.spawnActor(serviceInfo.NodeHost, kind, id)
}

// renewAllActors 批量续约所有 child actor（使用 Lua 脚本）
func (a *actorActivator) renewAllActors(ctx context.Context) {
	if len(a.childs) == 0 {
		return
	}
	keys := make([]string, 0, len(a.childs))
	pidInfos := make([]string, 0, len(a.childs))

	for pid, id := range a.childs {
		key := getActorLocateKey(a.kind, id)
		pidInfo, _ := protojson.Marshal(&pb.ActorPid{
			Address: pid.Address,
			Id:      pid.Id,
		})
		keys = append(keys, key)
		pidInfos = append(pidInfos, string(pidInfo))
	}
	if err := a.manager.locator.RegisterBatchActor(ctx, keys, pidInfos, ActorLocateTTL); err != nil {
		glog.Errorf(ctx, "renewAllActorNodes failed: %v", err)
	}
}

func getActorLocateKey(kind string, id string) string {
	return fmt.Sprintf("actor:%s:%s", kind, id)
}

func (g *activatorManager) GetActorCount(kind string) int {
	info, ok := g.activatorMetas[kind]
	if !ok {
		return 0
	}
	return info.mgr.Count()
}
