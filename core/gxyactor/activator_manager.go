package gxyactor

import (
	"context"
	"fmt"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxyredis"
	"gserver/core/gxyregistery"
	"gserver/core/gxyservice"
	"gserver/protocol/pb"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/asynkron/protoactor-go/remote"
	"github.com/asynkron/protoactor-go/router"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/redis/go-redis/v9"
)

const (
	ActorLocateTTL = 12 * time.Hour
)

// hashableActorActive wraps pb.ActorActive to implement router.Hasher
// so the consistent-hash pool can route by actor id.
type hashableActorActive struct {
	*pb.ActorActive
	hash string
}

func (m *hashableActorActive) Hash() string { return m.hash }

type activatorMeta struct {
	Kind      string
	Props     *actor.Props
	Activator PID // router PID (external entry point)
	Pool      PID // consistent-hash pool PID (internal)
	mgr       *ActorMgr
}

// activatorRouter is a thin proxy that receives pb.ActorActive from remote nodes
// and forwards them as hashableActorActive to the local consistent-hash pool.
type activatorRouter struct {
	*ActorBase
	poolPID PID
}

func NewActivatorRouter(poolPID PID) *activatorRouter {
	r := &activatorRouter{poolPID: poolPID}
	ctx := gxylog.NewContext(context.Background(), "activator_router")
	r.ActorBase = NewActorBase(ctx, r)
	return r
}

func (r *activatorRouter) HandleMessage(ctx context.Context, msg any) error {
	switch msg := msg.(type) {
	case *pb.ActorActive:
		sender := r.Actx.Sender()
		wrapped := &hashableActorActive{
			ActorActive: msg,
			hash:        msg.Id,
		}
		r.Actx.RequestWithCustomSender(r.poolPID, wrapped, sender)
	}
	return nil
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
	return nil
}

func (a *actorActivator) HandleMessage(ctx context.Context, msg any) error {
	switch msg := msg.(type) {
	case *hashableActorActive:
		props := a.meta.Props.Clone()
		pid, err := a.SpawnNamed(props, msg.Id, msg.Id)
		if err != nil {
			if err == actor.ErrNameExists {
				a.Respond(&remote.ActorPidResponse{Pid: pid})
				return nil
			}
			a.Respond(ActorError(err.Error()))
			return nil
		}

		// 注册 Redis
		key := getActorLocateKey(a.kind, msg.Id)
		if err := a.registerActor(a.ctx, key); err != nil {
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
		a.deRegisterActor(a.ctx, getActorLocateKey(a.kind, id))
		a.meta.mgr.Remove(id)
		delete(a.childs, child)
		return nil
	}
	return nil
}

func (a *actorActivator) Terminate(ctx context.Context, err error) {
	glog.Info(ctx, "actor activator stopped")
}

func (a *actorActivator) registerActor(ctx context.Context, key string) error {
	return gxyredis.Redis().Set(ctx, key, a.manager.nodeInstanceName, ActorLocateTTL).Err()
}

func (a *actorActivator) deRegisterActor(ctx context.Context, key string) {
	gxyredis.Redis().Del(ctx, key)
}

const redisLocatePrefix = "gserver:locate:node"

func getActorLocateKey(kind string, id string) string {
	return fmt.Sprintf("%s:%s:%s:%s", redisLocatePrefix, "actor", kind, id)
}

type activatorManager struct {
	gxymodule.ModuleBase
	nodeName         string
	nodeInstanceName string
	activatorMetas   map[string]*activatorMeta
	ctx              context.Context
}

func NewActivatorManager(nodeName string, nodeInstanceName string) *activatorManager {
	return &activatorManager{
		nodeName:         nodeName,
		nodeInstanceName: nodeInstanceName,
		activatorMetas:   make(map[string]*activatorMeta),
		ctx:              gxylog.NewContext(context.Background(), "activatorManager"),
	}
}

func (g *activatorManager) OnModInit(ctx context.Context) error {
	return nil
}

func (g *activatorManager) OnModStop(ctx context.Context) error {
	for _, info := range g.activatorMetas {
		StopActor(info.Activator)
		StopActor(info.Pool)
	}
	return nil
}

func (g *activatorManager) getPoolName(kind string) string {
	return fmt.Sprintf("%s_%s", "ActivatorPool", kind)
}

func (g *activatorManager) getRouterName(kind string) string {
	return fmt.Sprintf("%s_%s", "ActivatorRouter", kind)
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

	// Create consistent-hash pool (internal)
	poolPID, err := SpawnNamed(
		router.NewConsistentHashPool(5, actor.WithProducer(func() actor.Actor {
			return NewActorActivator(kind, g)
		})), g.getPoolName(kind))
	if err != nil {
		g.activatorMetas[kind] = nil
		return err
	}
	meta.Pool = poolPID

	// Create router (external entry point for remote nodes)
	routerPID, err := SpawnNamed(
		actor.PropsFromProducer(func() actor.Actor {
			return NewActivatorRouter(poolPID)
		}), g.getRouterName(kind))
	if err != nil {
		StopActor(poolPID)
		g.activatorMetas[kind] = nil
		return err
	}
	meta.Activator = routerPID
	meta.mgr = NewActorMgr(fmt.Sprintf("%s_%s", "actorMgr", kind))
	return nil
}

func (g *activatorManager) DeregisterActorKind(kind string) {
	info, ok := g.activatorMetas[kind]
	if !ok {
		return
	}
	StopActor(info.Activator)
	StopActor(info.Pool)
	delete(g.activatorMetas, kind)
}

func (g *activatorManager) spawnActor(node string, kind string, id string) (PID, error) {
	glog.Debugf(g.ctx, "spawn actor %s:%s at %s", kind, id, node)
	activator := actor.NewPID(node, g.getRouterName(kind))
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
	nodeName, err := gxyredis.Redis().Get(ctx, key).Result()
	if err != nil && err != redis.Nil {
		return nil, gerror.Wrap(err, "redis get failed")
	}

	if nodeName != "" {
		// Redis 中存的是 nodeInstanceName（game-2@uid），直接传给 Consul 查地址
		nodeHost := gxyservice.ServiceApp().GetAddressByNodeName(ctx, kind, nodeName)
		if nodeHost != "" {
			return g.spawnActor(nodeHost, kind, id)
		}
		// 节点已死 → fallback spawn（下面继续走）
		glog.Warningf(ctx, "node %s for actor %s not alive, re-spawning", nodeName, key)
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

func (g *activatorManager) GetActorCount(kind string) int {
	info, ok := g.activatorMetas[kind]
	if !ok {
		return 0
	}
	return info.mgr.Count()
}
