package gxyactor

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxymodule"
	"gserver/core/gxyredis"
	"gserver/core/gxyregistery"
	"gserver/core/gxyservice"
	"gserver/protocol/pb"
	"gserver/src/util"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/asynkron/protoactor-go/remote"
	"github.com/asynkron/protoactor-go/router"
	"github.com/cockroachdb/errors"
	"github.com/gogf/gf/v2/errors/gerror"
)

const (
	actorLocateMaxAttempts = 3
	// actorLocateRequestTimeout 必须大于 Touch 确认窗口(10s):
	// spawn 响应在 Init+Touch 完成后才返回,更短的超时会把慢初始化误判为失败。
	actorLocateRequestTimeout = 30 * time.Second
)

var errActorLocateRetryExhausted = errors.New("actor locate retry exhausted")

// hashableActorActive wraps pb.ActorActive to implement router.Hasher
// so the consistent-hash pool can route by actor id.
type hashableActorActive struct {
	*pb.ActorActive
	hash string
}

func (m *hashableActorActive) Hash() string { return m.hash }

type localMsgRegisterPool struct {
	unspanMessage
	Kind   string
	PoolID PID
}

type localMsgUnRegisterPool struct {
	unspanMessage
	Kind string
}

type localMsgActorTouchResult struct {
	unspanMessage
	ID    string
	PID   PID
	Owner ActorOwner
	Err   error
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
			return errors.Newf("pool %s not registered", msg.Kind)
		}
		CallSync(ctx, poolPID, wrapped, sender)
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

type pendingActivation struct {
	pid     PID
	owner   ActorOwner
	waiters []PID
}

type actorActivator struct {
	*ActorBase
	kind    string
	manager *activatorManager
	childs  map[PID]string
	owners  map[PID]ActorOwner
	pending map[string]*pendingActivation
	meta    *activatorMeta
}

func NewActorActivator(kind string, manager *activatorManager) *actorActivator {
	a := &actorActivator{
		kind:    kind,
		manager: manager,
		childs:  make(map[PID]string),
		owners:  make(map[PID]ActorOwner),
		pending: make(map[string]*pendingActivation),
	}
	ctx := gxylog.NewContext(context.Background(), "actor_activator")
	a.ActorBase = NewActorBase(ctx, a, "actor_activator")
	return a
}

type activationAction uint8

const (
	activationRetry activationAction = iota
	activationReturnLocal
	activationReleaseAndRetry
	activationSpawn
	activationConflict
)

// decideActivation 将 Claim 结果、本地 activation 状态和调用方的 spawn 意图
// 组合成唯一动作。localPID 来自当前节点 ActorMgr；nil 表示本地没有已登记的 Actor。
func decideActivation(owner ActorOwner, acquired bool, localNode string, localPID PID, allowSpawn bool) activationAction {
	// Claim 返回了其他节点的 owner：本节点不能创建或接管，只能重新定位。
	if owner.NodeID != localNode {
		return activationRetry
	}
	if acquired {
		// 本次 Claim 已抢到 owner，但本地已有同 ID Actor，说明 ownership
		// 状态与本地 activation 不一致，禁止继续创建第二个 Actor。
		if localPID != nil {
			return activationConflict
		}
		// 只有允许 spawn 的路径才能使用刚抢到的 owner 创建 Actor。
		if allowSpawn {
			return activationSpawn
		}
		// locate-only 请求不能创建 Actor；释放刚抢到的 owner 后重试。
		return activationReleaseAndRetry
	}
	// owner 属于本节点且 Claim 未抢占：本地 Actor 已存在，可直接返回。
	if localPID != nil {
		return activationReturnLocal
	}
	// owner 属于本节点但本地没有 Actor：这是残留 owner，条件释放后重试。
	return activationReleaseAndRetry
}

func (a *actorActivator) DelayInit(ctx context.Context) error {
	info, ok := a.manager.activatorMetas[a.kind]
	if !ok {
		return errors.Newf("actor kind %s not registered", a.kind)
	}
	a.meta = info
	return nil
}

func (a *actorActivator) unregisterActor(id string, pid PID) {
	owner := a.owners[pid]
	if _, err := a.manager.locator.release(a.ctx, a.kind, id, owner); err != nil {
		gxylog.Warn(a.ctx, "release actor owner failed", gxylog.Str("kind", a.kind), gxylog.Str("id", id), gxylog.Err(err))
	}
	a.meta.mgr.Remove(id)
	delete(a.childs, pid)
	delete(a.owners, pid)
}

func (a *actorActivator) HandleMessage(ctx context.Context, msg any) error {
	switch msg := msg.(type) {
	case *hashableActorActive:
		owner, acquired, err := a.manager.locator.claim(ctx, a.kind, msg.Id)
		if err != nil {
			_ = Respond(ctx, a.Actx, ActorError(err.Error()))
			return nil
		}
		if pending := a.pending[msg.Id]; pending != nil {
			if pending.owner != owner {
				_ = Respond(ctx, a.Actx, ActorError("pending actor activation lost ownership"))
				return nil
			}
			if sender := a.Actx.Sender(); sender != nil {
				pending.waiters = append(pending.waiters, sender)
			}
			return nil
		}
		localPID := a.meta.mgr.Get(msg.Id)
		switch decideActivation(owner, acquired, a.manager.nodeInstanceName, localPID, msg.GetAllowSpawn()) {
		case activationRetry:
			_ = Respond(ctx, a.Actx, &pb.ActorLocateRetry{})
			return nil
		case activationReturnLocal:
			_ = Respond(ctx, a.Actx, &remote.ActorPidResponse{Pid: localPID})
			return nil
		case activationReleaseAndRetry:
			if _, err := a.manager.locator.release(ctx, a.kind, msg.Id, owner); err != nil {
				_ = Respond(ctx, a.Actx, ActorError(err.Error()))
				return nil
			}
			_ = Respond(ctx, a.Actx, &pb.ActorLocateRetry{})
			return nil
		case activationConflict:
			_ = Respond(ctx, a.Actx, ActorError("claimed actor owner conflicts with an existing local activation"))
			return nil
		case activationSpawn:
		}

		props := a.meta.Props.Clone()
		// notice 这儿不要使用actor自带的SpawnNamed, 因为actor_context的spawn_named会把id偷偷的加上前缀，
		// 导致actor.NewPid(msg.Id)返回的pid和a.SpawnNamed(msg.Id)返回的pid不同
		pid, err := SpawnNamed(props, msg.Id, msg.Id, owner)
		if err != nil {
			_, releaseErr := a.manager.locator.release(ctx, a.kind, msg.Id, owner)
			if releaseErr != nil {
				err = errors.CombineErrors(err, releaseErr)
			}
			_ = Respond(ctx, a.Actx, ActorError(err.Error()))
			return nil
		}

		var waiters []PID
		if sender := a.Actx.Sender(); sender != nil {
			waiters = append(waiters, sender)
		}
		a.childs[pid] = msg.Id
		a.owners[pid] = owner
		a.pending[msg.Id] = &pendingActivation{
			pid:     pid,
			owner:   owner,
			waiters: waiters,
		}
		a.Actx.Watch(pid)

		// Touch may block on actor initialization. Only the result crosses back
		// into this activator's mailbox; actor state is never mutated here.
		self := a.Actx.Self()
		go func(id string, owner ActorOwner) {
			_, err := Call(context.Background(), pid, &actor.Touch{}, 10*time.Second)
			if sendErr := LocalSend(context.Background(), self, &localMsgActorTouchResult{
				ID: id, PID: pid, Owner: owner, Err: err,
			}); sendErr != nil {
				gxylog.Error(context.Background(), "deliver actor touch result failed",
					gxylog.Str("kind", a.kind), gxylog.Str("id", id), gxylog.Err(sendErr))
			}
		}(msg.Id, owner)

		return nil

	case *localMsgActorTouchResult:
		pending := a.pending[msg.ID]
		if pending == nil || pending.pid != msg.PID || pending.owner != msg.Owner {
			return nil
		}
		delete(a.pending, msg.ID)
		if msg.Err != nil {
			gxylog.Warn(ctx, "actor touch failed", gxylog.Str("kind", a.kind), gxylog.Str("id", msg.ID), gxylog.Err(msg.Err))
			_ = StopActor(msg.PID)
			a.unregisterActor(msg.ID, msg.PID)
			for _, waiter := range pending.waiters {
				_ = Send(ctx, waiter, ActorError("actor init failed or actor died"))
			}
			return nil
		}
		a.meta.mgr.Add(msg.ID, msg.PID)
		for _, waiter := range pending.waiters {
			_ = Send(ctx, waiter, &remote.ActorPidResponse{Pid: msg.PID})
		}
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
		if pending := a.pending[id]; pending != nil && pending.pid == child {
			delete(a.pending, id)
			for _, waiter := range pending.waiters {
				_ = Send(ctx, waiter, ActorError("actor terminated during initialization"))
			}
		}
		a.unregisterActor(id, child)
		return nil
	}
	return nil
}

func (a *actorActivator) Terminate(ctx context.Context, err error) {
	gxylog.Info(ctx, "actor activator stopped", gxylog.Err(err))
}

const redisLocatePrefix = "gserver:locate:node"

func getActorLocateKey(kind string, id string) string {
	return fmt.Sprintf("%s:%s:%s:%s", redisLocatePrefix, "actor", kind, id)
}

func getActorOwner(ctx context.Context, kind string, id string) (ActorOwner, error) {
	locator, err := activeActorLocator()
	if err != nil {
		return ActorOwner{}, err
	}
	return locator.locate(ctx, kind, id)
}

func getActorLocateNodeName(ctx context.Context, kind string, id string) (string, error) {
	owner, err := getActorOwner(ctx, kind, id)
	return owner.NodeID, err
}

func activeActorLocator() (*actorLocator, error) {
	if app != nil && app.activatorMgr != nil && app.activatorMgr.locator != nil {
		return app.activatorMgr.locator, nil
	}
	client := gxyredis.Redis()
	if client == nil {
		return nil, errors.New("actor locator Redis client is not initialized")
	}
	return newActorLocator(client, "", ""), nil
}

type activatorManager struct {
	gxymodule.ModuleBase
	nodeName         string
	nodeInstanceName string
	activatorMetas   map[string]*activatorMeta
	routerPID        PID
	ctx              context.Context
	serviceLookup    actorServiceLookup
	requestActorFunc func(ctx context.Context, node string, kind string, id string, allowSpawn bool) (PID, bool, error)
	locator          *actorLocator
	stopLease        func()
}

type actorServiceLookup interface {
	GetAddressByNodeName(ctx context.Context, name string, nodeInstanceName string) string
	GetServiceInfo(ctx context.Context, name string, key string, selector gxyregistery.ServiceSelector) *gxyregistery.ServiceInfo
}

func NewActivatorManager(nodeName string, nodeInstanceName string) *activatorManager {
	return &activatorManager{
		nodeName:         nodeName,
		nodeInstanceName: nodeInstanceName,
		activatorMetas:   make(map[string]*activatorMeta),
		ctx:              gxylog.NewContext(context.Background(), "activatorManager"),
		serviceLookup:    gxyservice.ServiceApp(),
		locator:          newActorLocator(gxyredis.Redis(), nodeInstanceName, nodeInstanceName),
	}
}

func (g *activatorManager) OnModInit(ctx context.Context) error {
	return nil
}

func (g *activatorManager) OnModStart(ctx context.Context) error {
	if err := g.locator.acquireNodeLease(ctx); err != nil {
		return err
	}
	g.stopLease = g.locator.startLeaseHeartbeat(ctx, func(err error) {
		gxylog.Fatal(ctx, "actor node lease lost; terminating process",
			gxylog.Str("node", g.nodeInstanceName),
			gxylog.Err(err),
		)
	})

	// Create router (external entry point for remote nodes)
	routerPID, err := SpawnNamed(
		actor.PropsFromProducer(func() actor.Actor {
			return NewActivatorRouter()
		}), g.getRouterName())
	if err != nil {
		g.stopLease()
		g.stopLease = nil
		_ = g.locator.releaseNodeLease(ctx)
		return err
	}
	g.routerPID = routerPID
	return nil
}

func (g *activatorManager) OnModStop(ctx context.Context) error {
	if g.stopLease != nil {
		g.stopLease()
		g.stopLease = nil
	}
	_ = g.locator.releaseNodeLease(ctx)
	_ = StopActor(g.routerPID)
	return nil
}

func (g *activatorManager) getPoolName(kind string) string {
	return fmt.Sprintf("%s_%s", "ActivatorPool", kind)
}

func (g *activatorManager) getRouterName() string {
	return "ActivatorRouter"
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
		})), g.getPoolName(kind))
	if err != nil {
		delete(g.activatorMetas, kind)
		return err
	}
	_ = LocalSend(g.ctx, g.routerPID, &localMsgRegisterPool{
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
	// 先停所有活跃 Actor，触发 Terminate → save → Redis 清理
	for _, pid := range info.mgr.All() {
		_ = StopActor(pid)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if info.mgr.Count() == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if n := info.mgr.Count(); n > 0 {
		gxylog.Warn(g.ctx, "actors still alive after drain", gxylog.Str("kind", kind), gxylog.Num("count", int64(n)))
	}

	_ = StopActor(info.Pool)
	_ = LocalSend(g.ctx, g.routerPID, &localMsgUnRegisterPool{
		Kind: kind,
	})
	delete(g.activatorMetas, kind)
}

func (g *activatorManager) requestActor(ctx context.Context, node string, kind string, id string, allowSpawn bool) (PID, bool, error) {
	gxylog.Debug(ctx, "request actor", gxylog.Str("kind", kind), gxylog.Str("id", id), gxylog.Str("node", node), gxylog.Bool("allow_spawn", allowSpawn))
	activator := actor.NewPID(node, g.getRouterName())
	rsp, err := Call(ctx, activator, &pb.ActorActive{
		Kind:       kind,
		Id:         id,
		AllowSpawn: allowSpawn,
	}, actorLocateRequestTimeout)
	if err != nil {
		return nil, false, err
	}
	switch rsp := rsp.(type) {
	case *pb.ActorLocateRetry:
		return nil, true, nil
	case *pb.ActorError:
		return nil, false, gerror.New(rsp.Reason)
	case *remote.ActorPidResponse:
		return rsp.Pid, false, nil
	default:
		return nil, false, errors.Newf("unexpected actor activation response: %T", rsp)
	}
}

func (g *activatorManager) getActor(ctx context.Context, kind string, id string, spawn bool) (PID, error) {
	result := "error"
	defer func() {
		gxymetrics.ActorLocate.WithLabelValues(kind, result).Inc()
	}()
	key := getActorLocateKey(kind, id)
	requestActor := g.requestActor
	if g.requestActorFunc != nil {
		requestActor = g.requestActorFunc
	}
	for range actorLocateMaxAttempts {
		owner, err := g.locator.locate(ctx, kind, id)
		if err != nil {
			return nil, err
		}
		if owner.NodeID != "" {
			nodeHost := g.serviceLookup.GetAddressByNodeName(ctx, kind, owner.NodeID)
			if nodeHost == "" {
				return nil, errors.Newf("active actor owner address unavailable: %s", owner.NodeID)
			}
			pid, retry, err := requestActor(ctx, nodeHost, kind, id, false)
			if retry {
				continue
			}
			if err != nil {
				return nil, err
			}
			result = "hit"
			return pid, nil
		}

		if !spawn {
			result = "not_found"
			return nil, gerror.Newf("actor kind:%s, id:%s not found", kind, id)
		}
		serviceInfo := g.serviceLookup.GetServiceInfo(ctx, kind, key, gxyregistery.ConsistentHashSelector())
		if serviceInfo == nil || serviceInfo.NodeHost == "" {
			return nil, gerror.Newf("find actor node failed, kind: %s, id: %s", kind, id)
		}
		pid, retry, err := requestActor(ctx, serviceInfo.NodeHost, kind, id, true)
		if retry {
			continue
		}
		if err != nil {
			return nil, err
		}
		result = "miss"
		return pid, nil
	}
	return nil, errActorLocateRetryExhausted
}

func (g *activatorManager) GetActorCount(kind string) int {
	info, ok := g.activatorMetas[kind]
	if !ok {
		return 0
	}
	return info.mgr.Count()
}

func (g *activatorManager) GetLocalActor(kind string, id string) PID {
	info, ok := g.activatorMetas[kind]
	if !ok {
		return nil
	}
	return info.mgr.Get(id)
}

func (g *activatorManager) GetLocalActorAll(kind string) []PID {
	info, ok := g.activatorMetas[kind]
	if !ok {
		return nil
	}
	return info.mgr.All()
}
