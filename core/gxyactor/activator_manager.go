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
	"gserver/core/gxyregistery/types"
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

	// ErrNotOwner 表示本次未取得该 actor 的归属:记录在别的节点,或本节点已有记录。
	// 这是正常的竞争结局,不是故障——调用方据此判断"归属不在我手里",而不是
	// "基础设施坏了"。
	// 由 actor 在同步初始化段获取所有权失败时返回,激活协调层据此重试。
	ErrNotOwner = errors.New("actor ownership not acquired")
)

// errLocateRetry 是"这一轮没有形成结论,重读记录再来"的唯一标记。
//
// 只有三类错误会被包成它 —— 全部是"记录/实例的状态刚变过",不是失败:
//  1. 陈旧记录刚被本节点条件释放      (resolveLocal)
//  2. 同名实例正在创建(ErrTaken)      (spawnLocal)
//  3. 归属被别的节点抢走(ErrNotOwner)  (actor 的同步初始化段)
//
// 其余错误一律不重试:未登记的 kind、Init 的确定性失败、目录/Redis 不可用——
// 重试只会把它们磨成"重试耗尽",盖掉真正的原因。
//
// 新增重试来源时只改这里:grep locateRetry( 就是全部需要重试的地方
// (本构造器 + 三个包装点 + 一个判断),不存在第四处。
//
// 原因必须随信号一起走,而不是靠"返回前记得打日志"的约定:
//   - 本进程内:原因就是错误消息本身(errors.Mark 只附加标记,不改文本);
//   - 跨节点:原因经 pb.ActorLocateRetry.Reason 带回,调用方原样记录。
var errLocateRetry = errors.New("actor locate retry")

// locateRetry 构造"这一轮没有形成结论"的错误。reason 是给人与日志看的原因,
// 它会作为错误文本一路带到重试日志与"重试耗尽"的错误消息里。
func locateRetry(reason string) error {
	return errors.Mark(errors.New(reason), errLocateRetry)
}

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

// activatorSupervisorKey 是托管激活协调者的监督者的身份。
var activatorSupervisorKey = actorKey{kind: "activator", id: "supervisor"}

// activatorSupervisor 只做一件事:让激活协调者始终在。
//
// 它不含业务逻辑——Init 只交出一份子进程清单,重启与上限由运行时的监督者机制承担。
// 因此它自身崩溃的面很小:这是"单点只是被上移一级"可被接受的理由(见 ADR 0019)。
type activatorSupervisor struct {
	act.Supervisor
	spec act.SupervisorSpec
}

func (s *activatorSupervisor) Init(_ ...any) (act.SupervisorSpec, error) {
	return s.spec, nil
}

// activatorSupervisorSpec 给出托管激活协调者的子进程清单。
//
// 用"总是重启":激活协调者没有需要恢复的状态,也不参与归属协议,因此重建它不会
// 与任何外部权威冲突——这正是它和承载实体的 actor 的分界(见 ADR 0019)。
func activatorSupervisorSpec(g *activatorManager) act.SupervisorSpec {
	routerCtor := func() ActorBehavior { return newActivatorActor(g) }
	return act.SupervisorSpec{
		Type: act.SupervisorTypeOneForOne,
		Restart: act.SupervisorRestart{
			Strategy: act.SupervisorStrategyPermanent,
		},
		Children: []act.SupervisorChildSpec{{
			Name:    activatorRouterKey.name(),
			Factory: ActorFactory(activatorRouterKey.kind, routerCtor),
		}},
	}
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
	stopLease func()

	serviceLookup actorServiceLookup
}

// actorServiceLookup 提供能力目录查询:哪些节点能承载该能力。
// 节点名到地址的解析不在这里 —— 那由运行时的注册适配器负责(见 ADR 0011)。
type actorServiceLookup interface {
	GetServiceInfo(ctx context.Context, name string, key string, selector gxyregistery.ServiceSelector) *types.ServiceInfo
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

	// 激活协调者交给监督者托管:它是本节点跨节点激活的单点,而节点只会拉起进程、
	// 不会重建崩溃的进程(见 ADR 0019)。
	supCtor := func() gen.ProcessBehavior { return &activatorSupervisor{spec: activatorSupervisorSpec(g)} }
	if _, err := app.spawnNamed(activatorSupervisorKey.name(), supCtor); err != nil {
		g.stopLease()
		g.stopLease = nil
		_ = g.lease.releaseNodeLease(ctx)
		return errors.Wrap(err, "spawn activator supervisor")
	}
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
	g.stopLocalActors(kind)
	if n := g.drainLocalActors(kind); n > 0 {
		gxylog.Warn(g.ctx, "actors still alive after drain",
			gxylog.Str("kind", kind), gxylog.Num("count", int64(n)))
	}
	delete(g.kinds, kind)
}

// stopLocalActors 停止该 kind 的全部本地实例。
func (g *activatorManager) stopLocalActors(kind string) {
	for _, pid := range g.localActors(kind) {
		_ = app.StopActor(pid)
	}
}

// drainLocalActors 等待本地实例全部停止,返回仍未停止的数量。
func (g *activatorManager) drainLocalActors(kind string) int {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && len(g.localActors(kind)) > 0 {
		time.Sleep(100 * time.Millisecond)
	}
	return len(g.localActors(kind))
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
// 返回 errLocateRetry 表示对端要求重新定位(记录指向别处,或刚被释放)。
func (g *activatorManager) requestActor(ctx context.Context, node string, k actorKey, allowSpawn bool) (PID, error) {
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
		return PID{}, err
	}
	switch rsp := rsp.(type) {
	case *pb.ActorLocateRetry:
		reason := rsp.GetReason()
		if reason == "" {
			reason = "no reason given"
		}
		return PID{}, locateRetry(fmt.Sprintf("node %s: %s", node, reason))
	case *pb.ActorError:
		return PID{}, gerror.New(rsp.Reason)
	case *pb.ActorPid:
		// 激活成功:用对端回报的地址,不按本地的 kind/id 重新拼一遍——
		// 该实例在对端节点上,对端才是权威。
		return PBToPid(rsp), nil
	default:
		return PID{}, errors.Newf("unexpected actor activation response: %T", rsp)
	}
}

// resolveLocal 在本地解析实例:有实例则返回;没有则条件释放陈旧记录,下一轮重新定位。
//
// 这是陈旧记录的自愈路径:所有权记录指向本节点但本节点已无实例时
// (例如释放时 Redis 失败留下残留),必须在这里清理,否则后续激活会
// 一直命中同一条记录而不收敛(见 invariants.md #8)。
func (g *activatorManager) resolveLocal(ctx context.Context, k actorKey, owner ActorOwner) (PID, error) {
	if pid := g.lookupLocal(k); !PIDIsZero(pid) {
		return pid, nil
	}
	// 本节点已是持有者但本地无实例:清理陈旧记录,下一轮重新定位。
	released, err := g.store.Release(ctx, k.kind, k.id, owner)
	if err != nil {
		return ZeroPID, errors.Wrap(err, "release stale actor owner")
	}
	gxylog.Info(ctx, "released stale actor owner",
		gxylog.Str("actor", k.String()), gxylog.Bool("released", released))
	return ZeroPID, locateRetry(fmt.Sprintf("released stale owner of %s", k))
}

// getActor 处理"记录已指向某个所有者"的情形:把解析交给所有者。
//
// 不得直接按名投递:必须经所有者节点校验它自己的本地实例,否则陈旧记录永远
// 不会被清理(见 invariants.md #8)。owner 指向本节点时走本地自愈路径。
func (g *activatorManager) getActor(ctx context.Context, k actorKey, owner ActorOwner) (PID, error) {
	if owner.NodeID == g.nodeID {
		return g.resolveLocal(ctx, k, owner)
	}
	return g.requestActor(ctx, owner.NodeID, k, false)
}

// spawnActor 处理"尚无任何所有者"的情形:决定由谁创建,并把创建请求送达。
//
// allowSpawn=false 表示调用方只查询、不创建,此时按不存在处理。候选节点由能力
// 目录的一致性哈希选出;候选就是本节点则直接创建,否则交给对方——实例在对端
// 节点上,对端才是权威。
func (g *activatorManager) spawnActor(ctx context.Context, k actorKey, allowSpawn bool) (PID, error) {
	if !allowSpawn {
		return ZeroPID, gerror.Newf("actor %s not found", k)
	}

	serviceInfo := g.serviceLookup.GetServiceInfo(ctx, k.kind, k.locateKey(), gxyregistery.ConsistentHashSelector())
	if serviceInfo == nil || serviceInfo.NodeHost == "" {
		return ZeroPID, gerror.Newf("find actor node failed, actor: %s", k)
	}
	// 候选节点就是本节点:直接创建,所有权由 actor 在同步初始化段获取。
	if serviceInfo.NodeName == g.nodeID {
		pid, err := g.spawnLocal(ctx, k)
		if isRaceLost(err) {
			return ZeroPID, locateRetry(fmt.Sprintf("instance of %s already spawning: %s", k, err))
		}
		return pid, err
	}
	return g.requestActor(ctx, serviceInfo.NodeName, k, allowSpawn)
}

// isRaceLost 判断这次创建失败是不是"竞争落败"。
//
// 落败有两种形态,都不是故障:注册名已被别的调用方占住,或归属记录已由本节点
// 抢到。两者都意味着"实例已经存在",重新定位一轮就能拿到它的地址。
//
// 其它失败(未登记的 kind、初始化报错、配置缺失)不是竞争:重试只会把它们磨成
// "重试耗尽",反而盖掉真正的原因,照旧直接上抛。
func isRaceLost(err error) bool {
	return errors.Is(err, ErrNotOwner) || errors.Is(err, gen.ErrTaken)
}

// activateActor 是激活的统一入口:反复定位直到拿到实例,或重试耗尽。
//
// 每轮都重新 Locate —— 上一轮可能刚清理掉一条陈旧记录,或归属刚被别的节点接管,
// 只有重读才知道当前事实。重试与失败处置只在这里发生一次:被调函数只回答
// "这一轮有没有形成结论",判据是 errLocateRetry(定义处列了三类来源)。
//
// 为什么不把所有失败都重试:确定性失败(kind 未登记、Init 因坏数据必然失败、
// 目录/Redis 不可用)重试 N 次也不会好,只会把真因磨成"重试耗尽"并放大 3×
// 的 Redis/RPC 负载。spawn 表示调用方是否允许创建。
func (g *activatorManager) activateActor(ctx context.Context, k actorKey, spawn bool) (PID, error) {
	result := "error"
	defer func() {
		gxymetrics.ActorLocate.WithLabelValues(k.kind, result).Inc()
	}()

	var lastRetry error
	for range actorLocateMaxAttempts {
		owner, err := g.store.Locate(ctx, k.kind, k.id)
		if err != nil {
			return ZeroPID, err
		}

		var pid PID
		if !owner.IsZero() {
			result = "get"
			pid, err = g.getActor(ctx, k, owner)
		} else {
			result = "spawn"
			pid, err = g.spawnActor(ctx, k, spawn)
		}
		if err == nil {
			return pid, nil
		}
		if !errors.Is(err, errLocateRetry) {
			result = "error"
			return ZeroPID, err
		}
		lastRetry = err
		gxylog.Debug(ctx, "actor locate retry", gxylog.Str("actor", k.String()), gxylog.Err(err))
	}
	result = "error"
	// 原因放进消息里(日志才看得到),同时保留"重试耗尽"的判定。
	return ZeroPID, errors.Mark(
		errors.Wrapf(lastRetry, "actor locate retry exhausted after %d attempts", actorLocateMaxAttempts),
		errActorLocateRetryExhausted,
	)
}

// spawnLocal 在本节点创建实例。初始化失败(含所有权未取得)时返回错误。
func (g *activatorManager) spawnLocal(_ context.Context, k actorKey) (PID, error) {
	factory, ok := g.kinds[k.kind]
	if !ok {
		return ZeroPID, errors.Newf("actor kind %s not registered", k.kind)
	}
	// 注册名与初始化参数都从身份派生:能力名由登记表的键写入实例,实例标识
	// 作为首个初始化参数交给它绑定。
	pid, err := app.spawnNamed(k.name(), factory, k.id)
	if err != nil {
		if errors.Is(err, ErrNotOwner) {
			return ZeroPID, err
		}
		return ZeroPID, errors.Wrapf(err, "spawn actor %s", k)
	}
	return pid, nil
}

type activatorActor struct {
	*BaseActor
	mgr *activatorManager
}

func newActivatorActor(mgr *activatorManager) *activatorActor {
	a := &activatorActor{mgr: mgr}
	// 激活协调者不承载实体:用基础层,类型上就取不到所有权(见 ADR 0015)。
	a.BaseActor = NewBaseActor()
	return a
}

// HandleCall 处理来自其他节点的激活请求(业务入口)。
// 用同步调用而非异步消息:激活需要立即拿到结果或明确的"换节点重试"。
func (a *activatorActor) HandleCall(ctx context.Context, msg any) (any, error) {
	switch req := msg.(type) {
	case *pb.ActorActive:
		return a.handleActive(ctx, req)
	}
	return nil, nil
}

// handleActive 处理一次激活请求:
//   - 本地已有实例 → 返回该实例的节点信息
//   - 本节点持有所有权但无实例 → 条件释放陈旧记录,让调用方重试
//   - 记录指向其他节点 → 让调用方重新定位
//   - 无记录 → 也让调用方重新定位(能不能创建由它的 spawn 标志决定)
//   - 允许创建且无权属 → 创建;竞争落败让调用方重试,确定性失败回业务错误
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
		if _, err := mgr.resolveLocal(ctx, k, owner); err != nil {
			if errors.Is(err, errLocateRetry) {
				// 原因随应答带回调用方,不需要"返回前记得打日志"。
				return &pb.ActorLocateRetry{Reason: err.Error()}, nil
			}
			return nil, err
		}
		return k.actorPid(mgr.nodeID), nil
	}

	// 记录指向其他节点:本节点不能创建,让调用方重新定位。
	if !owner.IsZero() {
		return &pb.ActorLocateRetry{
			Reason: fmt.Sprintf("record owned by %s", owner.NodeID),
		}, nil
	}

	// 无所有者:能不能创建由调用方决定,本节点不替它拒绝——
	// 让它重新定位,由它自己的 spawn 标志决定"不存在"还是"创建"。
	if !req.GetAllowSpawn() {
		return &pb.ActorLocateRetry{Reason: "record disappeared"}, nil
	}

	// 创建。所有权由 actor 在同步初始化段自行获取(ADR 0012)。
	if _, err := mgr.spawnLocal(ctx, k); err != nil {
		if isRaceLost(err) {
			return &pb.ActorLocateRetry{
				Reason: fmt.Sprintf("creation raced: %s", err),
			}, nil
		}
		// 确定性失败走业务错误通道:调用方拿到真因,且不会白白重试——
		// 重试只会把它磨成"重试耗尽",盖掉真正的原因。
		return nil, err
	}
	return k.actorPid(mgr.nodeID), nil
}
