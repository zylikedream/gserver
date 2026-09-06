package gxyactor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxymodule"
	"gserver/core/gxyredis"
	"gserver/core/gxyregistery"
	"gserver/core/gxyservice"
	"gserver/protocol/pb"

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

type actorActivationSpawner interface {
	spawnActivatorActor(kind, id string, owner ActorOwner) (PID, error)
	confirmActivatorActor(kind, id string, pid PID) error
	stopActivatorActor(pid PID) error
}

type ergoActivationSpawner struct{ manager *activatorManager }

func (s ergoActivationSpawner) spawnActivatorActor(kind, id string, owner ActorOwner) (PID, error) {
	runtime, err := currentRuntime()
	if err != nil {
		return PID{}, err
	}
	spawner, ok := runtime.(interface {
		Spawn(string, string, ActorProducer, ...any) (PID, error)
	})
	if !ok {
		return PID{}, errors.New("actor runtime does not support activation spawn")
	}
	meta := s.manager.activatorMetas[kind]
	if meta == nil || meta.Producer == nil {
		return PID{}, errors.Newf("actor kind %s is not registered", kind)
	}
	return spawner.Spawn(kind, id, meta.Producer, id, owner)
}

// Ergo waits for ProcessInit synchronously, so successful Spawn is the init confirmation.
func (ergoActivationSpawner) confirmActivatorActor(string, string, PID) error { return nil }
func (ergoActivationSpawner) stopActivatorActor(pid PID) error                { return StopActor(pid) }

type localMsgActorTouchResult struct {
	unspanMessage
	ID    string
	PID   PID
	Owner ActorOwner
	Err   error
}
type activatorMeta struct {
	Kind     string
	Producer ActorProducer
	mgr      *ActorMgr
	control  PID
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
	mu      sync.Mutex
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
// 组合成唯一动作。Claim 与 Spawn 是两个独立阶段：allowSpawn=false
// 仍会校验/清理 ownership，但绝不会执行 SpawnNamed。
// localPID 来自当前节点 ActorMgr；nil 表示本地没有已登记的 Actor。
func decideActivation(owner ActorOwner, acquired bool, localNode string, localPID PID, allowSpawn bool) activationAction {
	// Claim 返回了其他节点的 owner：本节点不能创建或接管，只能重新定位。
	if owner.NodeID != localNode {
		return activationRetry
	}
	if acquired {
		if !localPID.IsZero() {
			return activationConflict
		}
		if allowSpawn {
			return activationSpawn
		}
		return activationReleaseAndRetry
	}
	if !localPID.IsZero() {
		return activationReturnLocal
	}
	return activationReleaseAndRetry
}

func (a *actorActivator) requestLocal(ctx context.Context, id string, allowSpawn bool) (PID, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager == nil || a.manager.locator == nil {
		return PID{}, errors.New("actor locator is not initialized")
	}
	if a.meta == nil {
		a.meta = a.manager.activatorMetas[a.kind]
	}
	if a.meta == nil || a.meta.mgr == nil {
		return PID{}, errors.Newf("actor kind %s is not registered", a.kind)
	}
	localPID := a.meta.mgr.Get(id)
	if !allowSpawn && localPID.IsZero() {
		owner, err := a.manager.locator.locate(ctx, a.kind, id)
		if err != nil {
			return PID{}, err
		}
		if owner.NodeID == "" {
			return PID{}, gerror.Newf("actor kind:%s, id:%s not found", a.kind, id)
		}
	}
	owner, acquired, err := a.manager.locator.claim(ctx, a.kind, id)
	if err != nil {
		return PID{}, err
	}
	switch decideActivation(owner, acquired, a.manager.nodeInstanceName, localPID, allowSpawn) {
	case activationReturnLocal:
		return localPID, nil
	case activationRetry:
		return PID{}, errors.Newf("actor %s/%s is owned by %s", a.kind, id, owner.NodeID)
	case activationReleaseAndRetry:
		_, releaseErr := a.manager.locator.release(ctx, a.kind, id, owner)
		if releaseErr != nil {
			return PID{}, releaseErr
		}
		return PID{}, gerror.Newf("actor kind:%s, id:%s not found", a.kind, id)
	case activationConflict:
		return PID{}, errors.Newf("claimed actor owner conflicts with local activation: %s/%s", a.kind, id)
	case activationSpawn:
	}
	if a.manager.spawner == nil {
		_, _ = a.manager.locator.release(ctx, a.kind, id, owner)
		return PID{}, errors.New("actor activation spawner is not initialized")
	}
	pid, err := a.manager.spawner.spawnActivatorActor(a.kind, id, owner)
	if err != nil {
		_, releaseErr := a.manager.locator.release(ctx, a.kind, id, owner)
		if releaseErr != nil {
			return PID{}, errors.CombineErrors(err, releaseErr)
		}
		return PID{}, err
	}
	if err := a.manager.spawner.confirmActivatorActor(a.kind, id, pid); err != nil {
		_ = a.manager.spawner.stopActivatorActor(pid)
		_, releaseErr := a.manager.locator.release(ctx, a.kind, id, owner)
		if releaseErr != nil {
			return PID{}, errors.CombineErrors(err, releaseErr)
		}
		return PID{}, errors.Wrap(err, "actor init confirmation failed")
	}
	a.meta.mgr.Add(id, pid)
	a.childs[pid] = id
	a.owners[pid] = owner
	return pid, nil
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
	case *pb.ActorActive:
		// Claim 必须先于任何 SpawnNamed：Redis owner 是跨节点 single-writer
		// 的裁决结果，本地 ActorMgr 只能用于确认当前节点是否已有实例。
		owner, acquired, err := a.manager.locator.claim(ctx, a.kind, msg.Id)
		if err != nil {
			_ = Respond(ctx, a.Actx, ActorError(err.Error()))
			return nil
		}
		// 同一 ID 在 Touch 完成前再次到达时，加入同一个 pending activation。
		// 不重复 Claim/Spawn；owner 校验防止旧初始化结果接管新 owner。
		if pending := a.pending[msg.Id]; pending != nil {
			if pending.owner != owner {
				_ = Respond(ctx, a.Actx, ActorError("pending actor activation lost ownership"))
				return nil
			}
			if sender := a.Actx.Sender(); !sender.IsZero() {
				pending.waiters = append(pending.waiters, sender)
			}
			return nil
		}
		// Claim 后再检查本地 Actor，统一处理远程 owner、本地命中、
		// 残留 owner、重复 ownership 和允许/禁止 spawn 等分支。
		localPID := a.meta.mgr.Get(msg.Id)
		switch decideActivation(owner, acquired, a.manager.nodeInstanceName, localPID, msg.GetAllowSpawn()) {
		case activationRetry:
			_ = Respond(ctx, a.Actx, &pb.ActorLocateRetry{})
			return nil
		case activationReturnLocal:
			_ = Respond(ctx, a.Actx, &ActorPIDResponse{PID: localPID})
			return nil
		case activationReleaseAndRetry:
			// 只有 owner 完全匹配时 Release 才能删除记录，避免误删新 owner。
			if _, err := a.manager.locator.release(ctx, a.kind, msg.Id, owner); err != nil {
				_ = Respond(ctx, a.Actx, ActorError(err.Error()))
				return nil
			}
			_ = Respond(ctx, a.Actx, &pb.ActorLocateRetry{})
			return nil
		case activationConflict:
			// Claim 已成功但本地已有实例：宁可报错，也不能创建第二个 writer。
			_ = Respond(ctx, a.Actx, ActorError("claimed actor owner conflicts with an existing local activation"))
			return nil
		case activationSpawn:
			// 当前节点持有 owner 且允许创建，继续执行 Claim-before-Spawn。
		}

		// SpawnNamed 使用原始 ID，保证 Actor PID 与后续 ActorMgr 查找一致。
		if a.manager.spawner == nil {
			_, _ = a.manager.locator.release(ctx, a.kind, msg.Id, owner)
			_ = Respond(ctx, a.Actx, ActorError("actor activation spawner is not initialized"))
			return nil
		}
		pid, err := a.manager.spawner.spawnActivatorActor(a.kind, msg.Id, owner)
		if err != nil {
			// 创建失败也必须条件释放 owner，否则其他节点会看到残留 owner。
			_, releaseErr := a.manager.locator.release(ctx, a.kind, msg.Id, owner)
			if releaseErr != nil {
				err = errors.CombineErrors(err, releaseErr)
			}
			_ = Respond(ctx, a.Actx, ActorError(err.Error()))
			return nil
		}

		// 在异步 Init confirmation 完成前登记 pending；并发请求会在上面的分支加入 waiters。
		var waiters []PID
		if sender := a.Actx.Sender(); !sender.IsZero() {
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

		// Init/DelayInit 可能阻塞，不能占住 activator mailbox；结果通过本地消息
		// 回到 mailbox，继续由 Actor 顺序处理 pending 和 waiter。
		self := a.Actx.Self()
		go func(id string, owner ActorOwner) {
			err := a.manager.spawner.confirmActivatorActor(a.kind, id, pid)
			if sendErr := LocalSend(context.Background(), self, &localMsgActorTouchResult{
				ID: id, PID: pid, Owner: owner, Err: err,
			}); sendErr != nil {
				gxylog.Error(context.Background(), "deliver actor init confirmation failed",
					gxylog.Str("kind", a.kind), gxylog.Str("id", id), gxylog.Err(sendErr))
			}
		}(msg.Id, owner)

		return nil

		// Touch 结果必须回到 activator mailbox 串行处理；先校验 PID 和 owner，
		// 丢弃迟到的旧结果，避免旧 activation 修改新 pending。
	case *localMsgActorTouchResult:
		pending := a.pending[msg.ID]
		if pending == nil || pending.pid != msg.PID || pending.owner != msg.Owner {
			return nil
		}
		// pending 只在一次有效 Touch 结果到达后删除，之后该 Actor 才进入 mgr。
		delete(a.pending, msg.ID)
		// Touch 失败意味着 Actor 没有完成初始化：停止实例、条件释放 owner，
		// 再通知所有等待者，确保失败的 Actor 不会被当成可用实例返回。
		if msg.Err != nil {
			gxylog.Warn(ctx, "actor touch failed", gxylog.Str("kind", a.kind), gxylog.Str("id", msg.ID), gxylog.Err(msg.Err))
			_ = a.manager.spawner.stopActivatorActor(msg.PID)
			a.unregisterActor(msg.ID, msg.PID)
			for _, waiter := range pending.waiters {
				_ = Send(ctx, waiter, ActorError("actor init failed or actor died"))
			}
			return nil
		}
		// Touch 成功后才登记到 ActorMgr，随后把同一个 PID 返回给全部 waiters。
		a.meta.mgr.Add(msg.ID, msg.PID)
		for _, waiter := range pending.waiters {
			_ = Send(ctx, waiter, &ActorPIDResponse{PID: msg.PID})
		}
		return nil

	// Parent-child termination is delivered through the neutral lifecycle message.
	case ActorTerminatedMessage:
		child := msg.Who
		if child.IsZero() {
			return nil
		}
		id := a.childs[child]
		if id == "" {
			return nil
		}
		if pending := a.pending[id]; pending != nil && PidEqual(pending.pid, child) {
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
	ctx              context.Context
	serviceLookup    actorServiceLookup
	requestActorFunc func(ctx context.Context, node string, kind string, id string, allowSpawn bool) (PID, bool, error)
	locator          *actorLocator
	stopLease        func()
	spawner          actorActivationSpawner
}

type actorServiceLookup interface {
	GetAddressByNodeName(ctx context.Context, name string, nodeInstanceName string) string
	GetServiceInfo(ctx context.Context, name string, key string, selector gxyregistery.ServiceSelector) *gxyregistery.ServiceInfo
}

func NewActivatorManager(nodeName string, nodeInstanceName string) *activatorManager {
	g := &activatorManager{
		nodeName:         nodeName,
		nodeInstanceName: nodeInstanceName,
		activatorMetas:   make(map[string]*activatorMeta),
		ctx:              gxylog.NewContext(context.Background(), "activatorManager"),
		serviceLookup:    gxyservice.ServiceApp(),
		locator:          newActorLocator(gxyredis.Redis(), nodeInstanceName, nodeInstanceName),
	}
	g.spawner = ergoActivationSpawner{manager: g}
	return g
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

	return nil
}

func (g *activatorManager) OnModStop(ctx context.Context) error {
	if g.stopLease != nil {
		g.stopLease()
		g.stopLease = nil
	}
	_ = g.locator.releaseNodeLease(ctx)
	return nil
}

func (g *activatorManager) getActivatorName(kind string) string { return "Activator/" + kind }

func (g *activatorManager) RegisterActorKind(kind string, prod ActorProducer) error {
	runtime, err := currentRuntime()
	if err != nil {
		return err
	}
	register := runtime.RegisterActorKind
	if direct, ok := runtime.(interface {
		RegisterActorKindDirect(string, ActorProducer) error
	}); ok {
		register = direct.RegisterActorKindDirect
	}
	if err := register(kind, prod); err != nil {
		return err
	}
	meta := &activatorMeta{Kind: kind, Producer: prod, mgr: NewActorMgr(fmt.Sprintf("%s_%s", "actorMgr", kind))}
	g.activatorMetas[kind] = meta
	spawner, ok := runtime.(interface {
		SpawnNamed(string, string, ActorProducer, ...any) (PID, error)
	})
	if !ok {
		delete(g.activatorMetas, kind)
		runtime.DeregisterActorKind(kind)
		return errors.New("actor runtime does not support named activation")
	}
	meta.control, err = spawner.SpawnNamed("activator", g.getActivatorName(kind), func() IActor {
		activator := NewActorActivator(kind, g)
		activator.meta = meta
		return activator
	})
	if err != nil {
		delete(g.activatorMetas, kind)
		runtime.DeregisterActorKind(kind)
		return err
	}
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
	if !info.control.IsZero() {
		_ = StopActor(info.control)
	}

	delete(g.activatorMetas, kind)
}

func (g *activatorManager) requestActor(ctx context.Context, node string, kind string, id string, allowSpawn bool) (PID, bool, error) {
	gxylog.Debug(ctx, "request actor", gxylog.Str("kind", kind), gxylog.Str("id", id), gxylog.Str("node", node), gxylog.Bool("allow_spawn", allowSpawn))
	runtime, runtimeErr := currentRuntime()
	if runtimeErr != nil {
		return PID{}, false, runtimeErr
	}
	caller, ok := runtime.(interface {
		CallNamed(context.Context, string, string, any, time.Duration) (any, error)
	})
	if !ok {
		return PID{}, false, errors.New("actor runtime does not support named calls")
	}
	rsp, err := caller.CallNamed(ctx, node, g.getActivatorName(kind), &pb.ActorActive{Kind: kind, Id: id, AllowSpawn: allowSpawn}, actorLocateRequestTimeout)
	if err != nil {
		return PID{}, false, err
	}
	switch rsp := rsp.(type) {
	case *pb.ActorLocateRetry:
		return PID{}, true, nil
	case *pb.ActorError:
		return PID{}, false, gerror.New(rsp.Reason)
	case *ActorPIDResponse:
		return rsp.PID, false, nil
	default:
		return PID{}, false, errors.Newf("unexpected actor activation response: %T", rsp)
	}
}

func (g *activatorManager) ActivateActor(ctx context.Context, kind, id string, spawn bool) (PID, error) {
	return g.getActor(ctx, kind, id, spawn)
}
func (g *activatorManager) GetActorOwner(ctx context.Context, kind, id string) (ActorOwner, error) {
	return g.locator.locate(ctx, kind, id)
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
			return PID{}, err
		}
		if owner.NodeID != "" {
			nodeHost := g.serviceLookup.GetAddressByNodeName(ctx, kind, owner.NodeID)
			if nodeHost == "" {
				return PID{}, errors.Newf("active actor owner address unavailable: %s", owner.NodeID)
			}
			// 已有 owner 时这是 lookup-only 请求：即使 allowSpawn=false，
			// 远端仍需 Claim 重新校验 owner/lease，处理 locate 与请求之间的竞态。
			pid, retry, err := requestActor(ctx, nodeHost, kind, id, false)
			if retry {
				continue
			}
			if err != nil {
				return PID{}, err
			}
			result = "hit"
			return pid, nil
		}

		// 没有 owner 时，spawn=false 直接返回 not found，不会发送远程 Claim。
		if !spawn {
			result = "not_found"
			return PID{}, gerror.Newf("actor kind:%s, id:%s not found", kind, id)
		}
		serviceInfo := g.serviceLookup.GetServiceInfo(ctx, kind, key, gxyregistery.ConsistentHashSelector())
		if serviceInfo == nil || serviceInfo.NodeHost == "" {
			return PID{}, gerror.Newf("find actor node failed, kind: %s, id: %s", kind, id)
		}
		pid, retry, err := requestActor(ctx, serviceInfo.NodeHost, kind, id, true)
		if retry {
			continue
		}
		if err != nil {
			return PID{}, err
		}
		return pid, nil
	}
	return PID{}, errActorLocateRetryExhausted
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
		return PID{}
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
