package gxyactor

import (
	"context"
	"strings"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxymodule"
	"gserver/core/gxyredis"
	"gserver/core/gxyregistery"
	"gserver/core/gxyservice"
	"gserver/protocol/pb"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/cockroachdb/errors"
	"github.com/gogf/gf/v2/errors/gerror"
)

const actorLocateMaxAttempts = 3

var (
	errActorLocateRetryExhausted = errors.New("actor locate retry exhausted")

	// ErrNotOwner 表示本节点不是该 actor 的所有者。
	// 由 actor 在同步初始化段获取所有权失败时返回,激活协调层据此重试。
	ErrNotOwner = errors.New("actor ownership not acquired")
)

// actorName 返回进程的注册名的运行时表示。带 kind 前缀避免不同 kind 的相同 id 冲突。
func actorName(kind, id string) gen.Atom {
	return gen.Atom(kind + "/" + id)
}

type activatorManager struct {
	gxymodule.ModuleBase

	ctx       context.Context
	nodeName  string                        // 稳定节点名(与 kind 同名,用于按名推导地址)
	nodeID    string                        // 路由身份:node.name@host
	kinds     map[string]gen.ProcessFactory // kind → 创建该 kind 实例的工厂(工厂自带能力名)
	locator   *actorLocator
	routerPID PID
	stopLease func()

	serviceLookup actorServiceLookup

	// requestActorFunc 可替换函数变量:测试注入以隔离跨节点调用(编译期安全,非 gomonkey)。
	requestActorFunc func(ctx context.Context, node string, kind string, id string, allowSpawn bool) (PID, bool, error)
}

// actorServiceLookup 提供能力目录查询:哪些节点能承载该能力。
// 节点名到地址的解析不在这里 —— 那由运行时的注册适配器负责(见 ADR 0011)。
type actorServiceLookup interface {
	GetServiceInfo(ctx context.Context, name string, key string, selector gxyregistery.ServiceSelector) *gxyregistery.ServiceInfo
}

// NewActivatorManager 创建激活协调层。
// nodeInstanceName 保留参数以兼容调用处,内部改用运行时提供的节点名。
func NewActivatorManager(nodeName string, nodeInstanceName string) *activatorManager {
	nodeID := nodeInstanceName
	return &activatorManager{
		ctx:           gxylog.NewContext(context.Background(), "activatorManager"),
		nodeName:      nodeName,
		nodeID:        nodeID,
		kinds:         make(map[string]gen.ProcessFactory),
		serviceLookup: gxyservice.ServiceApp(),
		locator:       newActorLocator(gxyredis.Redis(), nodeID),
	}
}

func (g *activatorManager) OnModInit(ctx context.Context) error {
	return nil
}

func (g *activatorManager) OnModStart(ctx context.Context) error {
	// 租约获取失败即启动失败:同名节点的后继实例拿不到租约时不得启动,
	// 否则会静默夺走仍在运行的旧实例的租约(见 invariants.md #6)。
	if err := g.locator.acquireNodeLease(ctx); err != nil {
		return err
	}
	g.stopLease = g.locator.startLeaseHeartbeat(ctx, func(err error) {
		gxylog.Fatal(ctx, "actor node lease lost; terminating process",
			gxylog.Str("node", g.nodeID),
			gxylog.Err(err),
		)
	})

	routerProd := func() act.ActorBehavior { return newActivatorActor(g) }
	routerPID, err := app.spawnNamed(actorName("activator", "router"), asFactory("activator", routerProd))
	if err != nil {
		g.stopLease()
		g.stopLease = nil
		_ = g.locator.releaseNodeLease(ctx)
		return errors.Wrap(err, "spawn activator")
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
	return nil
}

// RegisterActorKind 登记一类 actor 的创建方式。
//
// 工厂在此处一次性构造并自带能力名:能力名必须与注册表的键一致(所有权键按它
// 区分),把它绑在登记动作上,创建路径就不必再重新推导一次。
func (g *activatorManager) RegisterActorKind(kind string, prod ActorProducer) error {
	g.kinds[kind] = asFactory(kind, prod)
	return nil
}

// DeregisterActorKind 停止该 kind 的全部本地实例并注销。
// 停止会触发各实例的终止路径(最终落库 + 所有权释放)。
func (g *activatorManager) DeregisterActorKind(kind string) {
	if _, ok := g.kinds[kind]; !ok {
		return
	}
	prefix := kind + "/"
	pids := g.localActors(kind)
	for _, pid := range pids {
		_ = app.StopActor(pid)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && len(g.localActors(kind)) > 0 {
		time.Sleep(100 * time.Millisecond)
	}
	if n := len(g.localActors(kind)); n > 0 {
		gxylog.Warn(g.ctx, "actors still alive after drain",
			gxylog.Str("kind", kind), gxylog.Num("count", int64(n)))
	}
	_ = prefix

	delete(g.kinds, kind)
}

// localActors 返回本节点上该 kind 的全部实例 PID。
func (g *activatorManager) localActors(kind string) []PID {
	if app == nil || app.node == nil {
		return nil
	}
	prefix := gen.Atom(kind + "/")
	var pids []PID
	_ = app.node.ProcessRangeShortInfo(func(info gen.ProcessShortInfo) bool {
		if strings.HasPrefix(string(info.Name), string(prefix)) {
			pids = append(pids, pidFromLocal(info.PID))
		}
		return true
	})
	return pids
}

// GetActorCount 返回本节点上该 kind 的实例数,用于服务注册的权重。
func (g *activatorManager) GetActorCount(kind string) int {
	return len(g.localActors(kind))
}

// GetLocalActor 返回本节点上该实例的 PID;不存在时返回零值。
// 查名是常数时间操作,不遍历进程表。
func (g *activatorManager) GetLocalActor(kind string, id string) PID {
	if app == nil || app.node == nil {
		return PID{}
	}
	pid, err := app.node.ProcessPID(actorName(kind, id))
	if err != nil {
		return PID{}
	}
	return pidFromLocal(pid)
}

// GetLocalActorAll 返回本节点上该 kind 的全部实例。
func (g *activatorManager) GetLocalActorAll(kind string) []PID {
	return g.localActors(kind)
}

// requestActor 向目标节点发起激活请求。
// node 是运行时节点名(不是主机地址):跨节点寻址必须先由注册信息把节点名
// 解析为可达地址(ADR 0011)。
// 返回 retry=true 表示需要换节点重新定位。
func (g *activatorManager) requestActor(ctx context.Context, node string, kind string, id string, allowSpawn bool) (PID, bool, error) {
	gxylog.Debug(ctx, "request actor",
		gxylog.Str("kind", kind), gxylog.Str("id", id),
		gxylog.Str("node", node), gxylog.Bool("allow_spawn", allowSpawn))

	target := gen.ProcessID{Name: actorName("activator", "router"), Node: gen.Atom(node)}
	rsp, err := app.callImportant(ctx, target, &pb.ActorActive{
		Kind:       kind,
		Id:         id,
		AllowSpawn: allowSpawn,
	})
	if err != nil {
		return PID{}, false, err
	}
	switch rsp := rsp.(type) {
	case *pb.ActorLocateRetry:
		return PID{}, true, nil
	case *pb.ActorError:
		return PID{}, false, gerror.New(rsp.Reason)
	case *pb.ActorPid:
		// 激活成功:实例归属该节点。
		return PID{}, false, nil
	default:
		return PID{}, false, errors.Newf("unexpected actor activation response: %T", rsp)
	}
}

// resolveLocal 在本地解析实例:有实例则返回,没有则条件释放陈旧记录并重试。
//
// 这是陈旧记录的自愈路径:所有权记录指向本节点但本节点已无实例时
// (例如释放时 Redis 失败留下残留),必须在这里清理,否则后续激活会
// 一直命中同一条记录而不收敛(见 invariants.md #8)。
func (g *activatorManager) resolveLocal(ctx context.Context, kind string, id string, owner ActorOwner) (PID, bool, error) {
	if pid := g.GetLocalActor(kind, id); !PIDIsZero(pid) {
		return pid, false, nil
	}
	// 本节点已是持有者但本地无实例:清理陈旧记录后重试。
	released, err := g.locator.release(ctx, kind, id, owner)
	if err != nil {
		return PID{}, false, errors.Wrap(err, "release stale actor owner")
	}
	gxylog.Info(ctx, "released stale actor owner",
		gxylog.Str("kind", kind), gxylog.Str("id", id), gxylog.Bool("released", released))
	return PID{}, true, nil
}

func (g *activatorManager) requestActorOrStub(ctx context.Context, node string, kind string, id string, allowSpawn bool) (PID, bool, error) {
	if g.requestActorFunc != nil {
		return g.requestActorFunc(ctx, node, kind, id, allowSpawn)
	}
	return g.requestActor(ctx, node, kind, id, allowSpawn)
}

func (g *activatorManager) getActor(ctx context.Context, kind string, id string, spawn bool) (PID, error) {
	result := "error"
	defer func() {
		gxymetrics.ActorLocate.WithLabelValues(kind, result).Inc()
	}()
	key := getActorLocateKey(kind, id)

	for range actorLocateMaxAttempts {
		owner, err := g.locator.locate(ctx, kind, id)
		if err != nil {
			return PID{}, err
		}

		if owner.NodeID != "" {
			// 已有所有者:必须经所有者节点校验本地实例,不得直接按名投递。
			// 否则陈旧记录永远不会被清理(见 invariants.md #8)。
			if owner.NodeID == g.nodeID {
				pid, retry, err := g.resolveLocal(ctx, kind, id, owner)
				if retry {
					continue
				}
				if err != nil {
					return PID{}, err
				}
				result = "hit"
				return pid, nil
			}
			// 所有权在本节点之外:交给所有者节点校验其本地实例。
			if _, retry, err := g.requestActorOrStub(ctx, owner.NodeID, kind, id, false); retry {
				continue
			} else if err != nil {
				return PID{}, err
			}
			result = "hit"
			return g.remoteRef(owner.NodeID, kind, id), nil
		}

		// 无所有者:spawn=false 时视为不存在,不创建。
		if !spawn {
			result = "not_found"
			return PID{}, gerror.Newf("actor kind:%s, id:%s not found", kind, id)
		}

		serviceInfo := g.serviceLookup.GetServiceInfo(ctx, kind, key, gxyregistery.ConsistentHashSelector())
		if serviceInfo == nil || serviceInfo.NodeHost == "" {
			return PID{}, gerror.Newf("find actor node failed, kind: %s, id: %s", kind, id)
		}
		// 候选节点就是本节点:直接创建,所有权由 actor 在同步初始化段获取。
		if serviceInfo.NodeName == g.nodeID {
			pid, err := g.spawnLocal(ctx, kind, id)
			if err != nil {
				return PID{}, err
			}
			result = "miss"
			return pid, nil
		}
		if _, retry, err := g.requestActorOrStub(ctx, serviceInfo.NodeName, kind, id, true); retry {
			continue
		} else if err != nil {
			return PID{}, err
		}
		result = "miss"
		return g.remoteRef(serviceInfo.NodeName, kind, id), nil
	}
	return PID{}, errActorLocateRetryExhausted
}

// claim 为 actor 获取所有权。由 actor 在同步初始化段调用(ADR 0012)。
func (g *activatorManager) claim(kind string, id string) (ActorOwner, error) {
	owner, acquired, err := g.locator.claim(context.Background(), kind, id)
	if err != nil {
		return ActorOwner{}, err
	}
	if !acquired {
		// 本节点已是持有者,说明这是前驱留下的记录;交由上层按陈旧记录处理。
		return ActorOwner{}, errors.Wrapf(ErrNotOwner, "kind=%s id=%s", kind, id)
	}
	return owner, nil
}

// release 释放所有权。
func (g *activatorManager) release(ctx context.Context, kind string, id string, owner ActorOwner) (bool, error) {
	return g.locator.release(ctx, kind, id, owner)
}

// spawnLocal 在本节点创建实例。初始化失败(含所有权未取得)时返回错误。
func (g *activatorManager) spawnLocal(_ context.Context, kind string, id string) (PID, error) {
	factory, ok := g.kinds[kind]
	if !ok {
		return PID{}, errors.Newf("actor kind %s not registered", kind)
	}
	pid, err := app.spawnNamed(actorName(kind, id), factory, id)
	if err != nil {
		if errors.Is(err, ErrNotOwner) {
			return PID{}, err
		}
		return PID{}, errors.Wrapf(err, "spawn actor %s/%s", kind, id)
	}
	return pid, nil
}

// remoteRef 返回远端实例的引用。
// 远端以"节点 + 注册名"寻址:运行时的进程标识跨节点会被代际校验,
// 对端重启后即失效;注册名是稳定的逻辑身份。
func (g *activatorManager) remoteRef(node string, kind string, id string) PID {
	return pidFromRemote(node, actorName(kind, id))
}

type activatorActor struct {
	*Actor
	mgr *activatorManager

	// from/ref 记录当前请求方,供回包使用。
	from gen.PID
	ref  gen.Ref
}

func newActivatorActor(mgr *activatorManager) *activatorActor {
	a := &activatorActor{mgr: mgr}
	// 激活协调者不承载实体:用基础层,类型上就取不到所有权(见 ADR 0015)。
	a.Actor = NewActor("activator")
	return a
}

// ReceiveCall 处理来自其他节点的激活请求(业务入口,见 ADR 0016)。
// 用同步调用而非异步消息:激活需要立即拿到结果或明确的"换节点重试"。
func (a *activatorActor) ReceiveCall(from gen.PID, ref gen.Ref, msg any) (any, error) {
	a.from, a.ref = from, ref
	req, ok := msg.(*pb.ActorActive)
	if !ok {
		return nil, nil
	}
	a.handleActive(a.Ctx, req)
	return nil, nil
}

// handleActive 处理一次激活请求:
//   - 本地已有实例 → 返回该实例的节点信息
//   - 本节点持有所有权但无实例 → 条件释放陈旧记录,让调用方重试
//   - 允许创建且无权属 → 创建;创建失败(含所有权竞争落败)让调用方重试
func (a *activatorActor) handleActive(ctx context.Context, req *pb.ActorActive) {
	mgr := a.mgr
	kind, id := req.GetKind(), req.GetId()

	owner, err := mgr.locator.locate(ctx, kind, id)
	if err != nil {
		a.reply(ActorError(err.Error()))
		return
	}

	// 记录指向本节点:以本地实例为准。
	if owner.NodeID == mgr.nodeID {
		_, retry, err := mgr.resolveLocal(ctx, kind, id, owner)
		if err != nil {
			a.reply(ActorError(err.Error()))
			return
		}
		if retry {
			a.reply(&pb.ActorLocateRetry{})
			return
		}
		a.replyPid(kind, id)
		return
	}

	// 记录指向其他节点:本节点不能创建,让调用方重新定位。
	if owner.NodeID != "" {
		a.reply(&pb.ActorLocateRetry{})
		return
	}

	// 无所有者且不允许创建:视为不存在。
	if !req.GetAllowSpawn() {
		a.reply(ActorError("actor not found and spawn not allowed"))
		return
	}

	// 创建。所有权由 actor 在同步初始化段自行获取(ADR 0012)。
	if _, err := mgr.spawnLocal(ctx, kind, id); err != nil {
		// 竞争落败或依赖不可用:让调用方换节点重试,而非当作硬失败。
		gxylog.Debug(ctx, "spawn actor failed, ask caller to retry",
			gxylog.Str("kind", kind), gxylog.Str("id", id), gxylog.Err(err))
		a.reply(&pb.ActorLocateRetry{})
		return
	}
	a.replyPid(kind, id)
}

func (a *activatorActor) replyPid(kind string, id string) {
	a.reply(&pb.ActorPid{Address: a.mgr.nodeID, Id: string(actorName(kind, id))})
}

// reply 向当前请求方回包。
func (a *activatorActor) reply(message any) {
	if err := a.ReplyCall(a.from, a.ref, message); err != nil {
		gxylog.Warn(a.Ctx, "activator reply failed", gxylog.Err(err))
	}
}
