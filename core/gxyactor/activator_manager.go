package gxyactor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxymodule"
	"gserver/core/gxyredis"
	"gserver/core/gxyregistery"
	"gserver/core/gxyservice"
	"gserver/protocol/pb"

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

// actorKey 是一个 actor 实例的逻辑身份:能力名 + 实例标识。
//
// 这两者的全部用途就是派生出该身份的各种表示——注册名、所有权记录的键、
// 跨节点寻址的目标、回给调用方的应答。因此它们作为一个值传递。
//
// 散着传两个字符串时,每个使用点都要各自拼一遍:拼错不会有编译期报错,只会让
// 实例注册到一个查不到的名字下,表现为它被反复重建。
type actorKey struct {
	kind string
	id   string
}

// name 是运行时的注册名。带 kind 前缀,避免不同 kind 的相同 id 冲突。
func (k actorKey) name() gen.Atom {
	return gen.Atom(k.kind + "/" + k.id)
}

// locateKey 是所有权记录在 Redis 中的键。
func (k actorKey) locateKey() string {
	return fmt.Sprintf("%s:%s:%s:%s", redisLocatePrefix, "actor", k.kind, k.id)
}

// String 返回可读表示,用于日志与错误信息。
func (k actorKey) String() string {
	return k.kind + "/" + k.id
}

// remoteRef 返回该实例在指定节点上的引用。
//
// 以"节点 + 注册名"寻址:运行时的进程标识跨节点会被代际校验,对端重启后即失效;
// 注册名是稳定的逻辑身份。
func (k actorKey) remoteRef(node string) PID {
	return pidFromRemote(node, k.name())
}

// actorPid 构造本节点对该实例的应答(节点 + 注册名),用于回复激活请求。
//
// Name 装注册名而不是实例标识:跨节点只能按名寻址,见 PBToPid。
func (k actorKey) actorPid(nodeID string) *pb.ActorPid {
	return &pb.ActorPid{Address: nodeID, Name: string(k.name())}
}

// activatorRouterKey 是激活协调者自身的身份:每节点一个,处理跨节点的激活请求。
//
// 它是一个具名身份而非散落的字符串——注册与寻址两处必须一致,而这两处曾经
// 各写了一次 "activator"/"router"。
var activatorRouterKey = actorKey{kind: "activator", id: "router"}

type activatorManager struct {
	gxymodule.ModuleBase

	ctx context.Context
	// nodeID 是节点身份:运行时节点名,也是服务注册与所有权记录里的节点标识(ADR 0018)。
	nodeID string
	kinds  map[string]gen.ProcessFactory // kind → 创建该 kind 实例的工厂(工厂自带能力名)
	// store 与 lease 是两个不同的关注点,不是一个东西的两半:
	//   - store:每个 actor 的归属记录,实例级生灭,可注入(见 OwnershipStore);
	//   - lease:本节点租约,模块级生灭,只有一种实现。
	// 生产环境两者由同一个 Redis 定位器承担——它们共用同一份 keyspace 与
	// 一致性视图,拆成两个独立存储会让"谁持有"与"谁有权持有"失去共同判据。
	store     OwnershipStore
	lease     nodeLease
	routerPID PID
	stopLease func()

	serviceLookup actorServiceLookup

	// requestActorFunc 可替换函数变量:测试注入以隔离跨节点调用(编译期安全,非 gomonkey)。
	requestActorFunc func(ctx context.Context, node string, k actorKey, allowSpawn bool) (PID, bool, error)
}

// actorServiceLookup 提供能力目录查询:哪些节点能承载该能力。
// 节点名到地址的解析不在这里 —— 那由运行时的注册适配器负责(见 ADR 0011)。
type actorServiceLookup interface {
	GetServiceInfo(ctx context.Context, name string, key string, selector gxyregistery.ServiceSelector) *gxyregistery.ServiceInfo
}

// NewActivatorManager 创建激活协调层。nodeID 即节点身份(见 ADR 0018)。
func NewActivatorManager(nodeID string) *activatorManager {
	locator := newActorLocator(gxyredis.Redis(), nodeID)
	return &activatorManager{
		ctx:           gxylog.NewContext(context.Background(), "activatorManager"),
		nodeID:        nodeID,
		kinds:         make(map[string]gen.ProcessFactory),
		serviceLookup: gxyservice.ServiceApp(),
		store:         locator,
		lease:         locator,
	}
}

func (g *activatorManager) OnModInit(ctx context.Context) error {
	return nil
}

func (g *activatorManager) OnModStart(ctx context.Context) error {
	// 租约获取失败即启动失败:同名节点的后继实例拿不到租约时不得启动,
	// 否则会静默夺走仍在运行的旧实例的租约(见 invariants.md #6)。
	if err := g.lease.acquireNodeLease(ctx); err != nil {
		return err
	}
	g.stopLease = g.lease.startLeaseHeartbeat(ctx, func(err error) {
		gxylog.Fatal(ctx, "actor node lease lost; terminating process",
			gxylog.Str("node", g.nodeID),
			gxylog.Err(err),
		)
	})

	routerCtor := func() Business { return newActivatorActor(g) }
	routerPID, err := app.spawnNamed(activatorRouterKey.name(), ActorFactory(activatorRouterKey.kind, routerCtor))
	if err != nil {
		g.stopLease()
		g.stopLease = nil
		_ = g.lease.releaseNodeLease(ctx)
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
	_ = g.lease.releaseNodeLease(ctx)
	return nil
}

// RegisterActorKind 登记一类 actor 的创建方式。
//
// 工厂在此处一次性构造并自带能力名:能力名必须与注册表的键一致(所有权键按它
// 区分),把它绑在登记动作上,创建路径就不必再重新推导一次。
func (g *activatorManager) RegisterActorKind(kind string, ctor ActorConstructor) error {
	g.kinds[kind] = ActorFactory(kind, ctor)
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
	return g.lookupLocal(actorKey{kind: kind, id: id})
}

// lookupLocal 按身份查本节点实例;不存在时返回零值。
// 查名是常数时间操作,不遍历进程表。
func (g *activatorManager) lookupLocal(k actorKey) PID {
	if app == nil || app.node == nil {
		return PID{}
	}
	pid, err := app.node.ProcessPID(k.name())
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
func (g *activatorManager) requestActor(ctx context.Context, node string, k actorKey, allowSpawn bool) (PID, bool, error) {
	gxylog.Debug(ctx, "request actor",
		gxylog.Str("actor", k.String()),
		gxylog.Str("node", node), gxylog.Bool("allow_spawn", allowSpawn))

	target := gen.ProcessID{Name: activatorRouterKey.name(), Node: gen.Atom(node)}
	rsp, err := app.callImportant(ctx, target, &pb.ActorActive{
		Kind:       k.kind,
		Id:         k.id,
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
		// 激活成功:用对端回报的地址,不按本地的 kind/id 重新拼一遍——
		// 该实例在对端节点上,对端才是权威。
		return PBToPid(rsp), false, nil
	default:
		return PID{}, false, errors.Newf("unexpected actor activation response: %T", rsp)
	}
}

// resolveLocal 在本地解析实例:有实例则返回,没有则条件释放陈旧记录并重试。
//
// 这是陈旧记录的自愈路径:所有权记录指向本节点但本节点已无实例时
// (例如释放时 Redis 失败留下残留),必须在这里清理,否则后续激活会
// 一直命中同一条记录而不收敛(见 invariants.md #8)。
func (g *activatorManager) resolveLocal(ctx context.Context, k actorKey, owner ActorOwner) (PID, bool, error) {
	if pid := g.lookupLocal(k); !PIDIsZero(pid) {
		return pid, false, nil
	}
	// 本节点已是持有者但本地无实例:清理陈旧记录后重试。
	released, err := g.store.Release(ctx, k.kind, k.id, owner)
	if err != nil {
		return PID{}, false, errors.Wrap(err, "release stale actor owner")
	}
	gxylog.Info(ctx, "released stale actor owner",
		gxylog.Str("actor", k.String()), gxylog.Bool("released", released))
	return PID{}, true, nil
}

func (g *activatorManager) requestActorOrStub(ctx context.Context, node string, k actorKey, allowSpawn bool) (PID, bool, error) {
	if g.requestActorFunc != nil {
		return g.requestActorFunc(ctx, node, k, allowSpawn)
	}
	return g.requestActor(ctx, node, k, allowSpawn)
}

func (g *activatorManager) getActor(ctx context.Context, k actorKey, spawn bool) (PID, error) {
	result := "error"
	defer func() {
		gxymetrics.ActorLocate.WithLabelValues(k.kind, result).Inc()
	}()

	for range actorLocateMaxAttempts {
		owner, err := g.store.Locate(ctx, k.kind, k.id)
		if err != nil {
			return PID{}, err
		}

		if owner.NodeID != "" {
			// 已有所有者:必须经所有者节点校验本地实例,不得直接按名投递。
			// 否则陈旧记录永远不会被清理(见 invariants.md #8)。
			if owner.NodeID == g.nodeID {
				pid, retry, err := g.resolveLocal(ctx, k, owner)
				if retry {
					continue
				}
				if err != nil {
					return PID{}, err
				}
				result = "hit"
				return pid, nil
			}
			// 所有权在本节点之外:交给所有者节点校验其本地实例,用它的应答。
			pid, retry, err := g.requestActorOrStub(ctx, owner.NodeID, k, false)
			if retry {
				continue
			}
			if err != nil {
				return PID{}, err
			}
			result = "hit"
			return pid, nil
		}

		// 无所有者:spawn=false 时视为不存在,不创建。
		if !spawn {
			result = "not_found"
			return PID{}, gerror.Newf("actor %s not found", k)
		}

		serviceInfo := g.serviceLookup.GetServiceInfo(ctx, k.kind, k.locateKey(), gxyregistery.ConsistentHashSelector())
		if serviceInfo == nil || serviceInfo.NodeHost == "" {
			return PID{}, gerror.Newf("find actor node failed, actor: %s", k)
		}
		// 候选节点就是本节点:直接创建,所有权由 actor 在同步初始化段获取。
		if serviceInfo.NodeName == g.nodeID {
			pid, err := g.spawnLocal(ctx, k)
			if err != nil {
				return PID{}, err
			}
			result = "miss"
			return pid, nil
		}
		pid, retry, err := g.requestActorOrStub(ctx, serviceInfo.NodeName, k, true)
		if retry {
			continue
		}
		if err != nil {
			return PID{}, err
		}
		result = "miss"
		return pid, nil
	}
	return PID{}, errActorLocateRetryExhausted
}

// spawnLocal 在本节点创建实例。初始化失败(含所有权未取得)时返回错误。
func (g *activatorManager) spawnLocal(_ context.Context, k actorKey) (PID, error) {
	factory, ok := g.kinds[k.kind]
	if !ok {
		return PID{}, errors.Newf("actor kind %s not registered", k.kind)
	}
	// 注册名与初始化参数都从身份派生:能力名由登记表的键写入实例,实例标识
	// 作为首个初始化参数交给它绑定。
	pid, err := app.spawnNamed(k.name(), factory, k.id)
	if err != nil {
		if errors.Is(err, ErrNotOwner) {
			return PID{}, err
		}
		return PID{}, errors.Wrapf(err, "spawn actor %s", k)
	}
	return pid, nil
}

type activatorActor struct {
	*Actor
	mgr *activatorManager
}

func newActivatorActor(mgr *activatorManager) *activatorActor {
	a := &activatorActor{mgr: mgr}
	// 激活协调者不承载实体:用基础层,类型上就取不到所有权(见 ADR 0015)。
	a.Actor = NewActor()
	return a
}

// HandleCall 处理来自其他节点的激活请求(业务入口)。
// 用同步调用而非异步消息:激活需要立即拿到结果或明确的"换节点重试"。
func (a *activatorActor) HandleCall(msg any) (any, error) {
	switch req := msg.(type) {
	case *pb.ActorActive:
		return a.handleActive(a.Ctx, req)
	}
	return nil, nil
}

// handleActive 处理一次激活请求:
//   - 本地已有实例 → 返回该实例的节点信息
//   - 本节点持有所有权但无实例 → 条件释放陈旧记录,让调用方重试
//   - 允许创建且无权属 → 创建;创建失败(含所有权竞争落败)让调用方重试
//
// 返回值即应答;返回 error 由门面转成业务错误响应交给调用方。
func (a *activatorActor) handleActive(ctx context.Context, req *pb.ActorActive) (any, error) {
	mgr := a.mgr
	k := actorKey{kind: req.GetKind(), id: req.GetId()}

	owner, err := mgr.store.Locate(ctx, k.kind, k.id)
	if err != nil {
		return nil, err
	}

	// 记录指向本节点:以本地实例为准。
	if owner.NodeID == mgr.nodeID {
		_, retry, err := mgr.resolveLocal(ctx, k, owner)
		if err != nil {
			return nil, err
		}
		if retry {
			return &pb.ActorLocateRetry{}, nil
		}
		return k.actorPid(mgr.nodeID), nil
	}

	// 记录指向其他节点:本节点不能创建,让调用方重新定位。
	if owner.NodeID != "" {
		return &pb.ActorLocateRetry{}, nil
	}

	// 无所有者且不允许创建:视为不存在。
	if !req.GetAllowSpawn() {
		return nil, errors.New("actor not found and spawn not allowed")
	}

	// 创建。所有权由 actor 在同步初始化段自行获取(ADR 0012)。
	if _, err := mgr.spawnLocal(ctx, k); err != nil {
		// 竞争落败或依赖不可用:让调用方换节点重试,而非当作硬失败。
		gxylog.Debug(ctx, "spawn actor failed, ask caller to retry",
			gxylog.Str("actor", k.String()), gxylog.Err(err))
		return &pb.ActorLocateRetry{}, nil
	}
	return k.actorPid(mgr.nodeID), nil
}
