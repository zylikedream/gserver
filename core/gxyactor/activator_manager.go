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
	"gserver/src/util"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/asynkron/protoactor-go/remote"
	"github.com/asynkron/protoactor-go/router"
	"github.com/gogf/gf/v2/errors/gerror"
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

type localMsgRegisterPool struct {
	Kind   string
	PoolID PID
}

type localMsgUnRegisterPool struct {
	Kind string
}

type activatorMeta struct {
	Kind  string
	Props *actor.Props
	Pool  PID // consistent-hash pool PID (internal)
	mgr   *ActorMgr
}

// activatorRouter is a thin proxy that receives pb.ActorActive from remote nodes
// and forwards them as hashableActorActive to the local consistent-hash pool.
type routerMeta struct {
	Kind string
	PID  PID
}
type activatorRouter struct {
	*ActorBase
	poolPIDs []routerMeta
}

func NewActivatorRouter() *activatorRouter {
	r := &activatorRouter{}
	ctx := gxylog.NewContext(context.Background(), "activator_router")
	r.ActorBase = NewActorBase(ctx, r, "activator_router")
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
		poolPID := r.GetPool(msg.Kind)
		if poolPID == nil {
			return fmt.Errorf("pool %s not registered", msg.Kind)
		}
		r.Actx.RequestWithCustomSender(poolPID, wrapped, sender)
	case *localMsgRegisterPool:
		r.RegisterPool(msg.Kind, msg.PoolID)
	case *localMsgUnRegisterPool:
		r.UnRegisterPool(msg.Kind)
	}
	return nil
}

func (r *activatorRouter) RegisterPool(kind string, poolPID PID) {
	r.poolPIDs = append(r.poolPIDs, struct {
		Kind string
		PID  PID
	}{kind, poolPID})
}

func (r *activatorRouter) UnRegisterPool(kind string) {
	r.poolPIDs = util.ListDeleteFunc(r.poolPIDs, func(item routerMeta) bool {
		return item.Kind == kind
	})
}

func (r *activatorRouter) GetPool(kind string) PID {
	for _, p := range r.poolPIDs {
		if p.Kind == kind {
			return p.PID
		}
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
	a.ActorBase = NewActorBase(ctx, a, "actor_activator")
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

func (a *actorActivator) registerActor(id string, pid PID) error {
	// 注册 Redis
	if err := a.registerActorLocate(a.ctx, getActorLocateKey(a.kind, id)); err != nil {
		return err
	}

	// 注册成功，加入 childs
	a.childs[pid] = id
	a.meta.mgr.Add(id, pid)
	return nil
}

func (a *actorActivator) unregisterActor(id string, pid PID) {
	a.deRegisterActorLocate(a.ctx, getActorLocateKey(a.kind, id))
	a.meta.mgr.Remove(id)
	delete(a.childs, pid)
}

func (a *actorActivator) HandleMessage(ctx context.Context, msg any) error {
	switch msg := msg.(type) {
	case *hashableActorActive:
		props := a.meta.Props.Clone()
		// notice 这儿不要使用a.SpawnNamed, 因为actor_context的spawn_named会把id偷偷的加上前缀，
		// 导致actor.NewPid(msg.Id)返回的pid和a.SpawnNamed(msg.Id, msg.Id)返回的pid不同
		pid, err := SpawnNamed(props, msg.Id, msg.Id)
		if err != nil {
			if err == actor.ErrNameExists {
				a.Respond(&remote.ActorPidResponse{Pid: pid})
				return nil
			}
			a.Respond(ActorError(err.Error()))
			return nil
		}

		// 注册 Redis
		if err := a.registerActor(msg.Id, pid); err != nil {
			StopActor(pid)
			a.Respond(&pb.ActorError{Reason: fmt.Sprintf("registration failed, key taken by another node, err: %s", err.Error())})
			return nil
		}

		// Touch 确认（异步，验证 Init 是否成功）
		sender := a.Actx.Sender()
		go func(id string) {
			if _, err := Call(context.Background(), pid, &actor.Touch{}, 2*time.Second); err != nil {
				a.unregisterActor(id, pid)
				Send(context.Background(), sender, ActorError("actor init failed or actor died"))
				return
			}
			a.Actx.Watch(pid)
			Send(context.Background(), sender, &remote.ActorPidResponse{Pid: pid})
		}(msg.Id)

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
		a.unregisterActor(id, child)
		return nil
	}
	return nil
}

func (a *actorActivator) Terminate(ctx context.Context, err error) {
	gxylog.Info(ctx, "actor activator stopped", gxylog.Err(err))
}

func (a *actorActivator) registerActorLocate(ctx context.Context, key string) error {
	return gxyredis.Redis().Set(ctx, key, a.manager.nodeInstanceName, ActorLocateTTL).Err()
}

func (a *actorActivator) deRegisterActorLocate(ctx context.Context, key string) {
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
	routerPID        PID
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

func (g *activatorManager) OnModStart(ctx context.Context) error {
	// Create router (external entry point for remote nodes)
	routerPID, err := SpawnNamed(
		actor.PropsFromProducer(func() actor.Actor {
			return NewActivatorRouter()
		}), g.getRouterName())
	if err != nil {
		return err
	}
	g.routerPID = routerPID
	return nil
}

func (g *activatorManager) OnModStop(ctx context.Context) error {
	for _, info := range g.activatorMetas {
		StopActor(info.Pool)
	}
	return nil
}

func (g *activatorManager) getPoolName(kind string) string {
	return fmt.Sprintf("%s_%s", "ActivatorPool", kind)
}

func (g *activatorManager) getRouterName() string {
	return fmt.Sprintf("%s", "ActivatorRouter")
}

func (g *activatorManager) RegisterActorKind(kind string, prod ActorProducer) error {
	actorProps := actor.PropsFromProducer(func() actor.Actor {
		return prod()
	}, actor.WithSupervisor(newSupervisor()))
	meta := &activatorMeta{
		Kind:  kind,
		Props: actorProps,
	}

	meta.mgr = NewActorMgr(fmt.Sprintf("%s_%s", "actorMgr", kind))
	g.activatorMetas[kind] = meta

	// Create consistent-hash pool (internal)
	poolPID, err := SpawnNamed(
		router.NewConsistentHashPool(5, actor.WithProducer(func() actor.Actor {
			return NewActorActivator(kind, g)
		}), actor.WithSenderMiddleware(tracePropagationMiddleware())), g.getPoolName(kind))
	if err != nil {
		delete(g.activatorMetas, kind)
		return err
	}
	LocalSend(g.ctx, g.routerPID, &localMsgRegisterPool{
		Kind:   kind,
		PoolID: poolPID,
	})
	meta.Pool = poolPID
	return nil
}

func (g *activatorManager) DeregisterActorKind(kind string) {
	info, ok := g.activatorMetas[kind]
	if !ok {
		return
	}
	StopActor(info.Pool)
	LocalSend(g.ctx, g.routerPID, &localMsgUnRegisterPool{
		Kind: kind,
	})
	delete(g.activatorMetas, kind)
}

func (g *activatorManager) spawnActor(node string, kind string, id string) (PID, error) {
	gxylog.Debug(g.ctx, "spawn actor", gxylog.Str("kind", kind), gxylog.Str("id", id), gxylog.Str("node", node))
	activator := actor.NewPID(node, g.getRouterName())
	rsp, err := Call(g.ctx, activator, &pb.ActorActive{
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
			return actor.NewPID(nodeHost, id), nil
		}
		// 节点已死 → fallback spawn（下面继续走）
		gxylog.Warn(ctx, "node not alive, re-spawning", gxylog.Str("node", nodeName), gxylog.Str("actor", key))
	}

	if !spawn {
		return nil, gerror.Newf("actor kind:%s, id:%s not found", kind, id)
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
