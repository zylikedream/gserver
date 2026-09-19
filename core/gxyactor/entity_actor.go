package gxyactor

import (
	"context"
	"strconv"

	"github.com/cockroachdb/errors"

	"gserver/core/gxylog"
	"gserver/core/gxyutil"

	"ergo.services/ergo/gen"
)

// EntityActor 是**承载持久实体**的 actor 的基类:在 Actor 之上加上所有权。
//
// 为什么单独一层(见 ADR 0015):代表一个需要单写者保护的实体,就必然要声明
// 归属——这不是可选项。把所有权放在只参与运行时的 Actor 上,会让会话、激活
// 协调者这类 actor 也继承一整套用不到的机制,而且"是否承载实体"会退化成
// "创建时恰好有没有传参数"这一运行期约定。
//
// 内嵌本类型即表示:本 actor 承载实体,因此参与所有权协议。
type EntityActor struct {
	*Actor

	msgHandler *gxyutil.MsgHandler

	// 本次激活持有的所有权,在同步初始化段获取、终止路径释放。
	owner   ActorOwner
	ownedID string
}

// NewEntityActor 创建承载实体的基类。
// biz 是用于消息分派的业务对象,通常是内嵌本基类的结构体自身。
func NewEntityActor(kind string, biz any) *EntityActor {
	return &EntityActor{
		Actor:      NewActor(kind, biz),
		msgHandler: gxyutil.NewMsgHandler(),
	}
}

// Receive 是业务入口(异步消息):按消息类型反射分派到业务方法。
func (e *EntityActor) Receive(_ gen.PID, message any) (any, error) {
	return e.dispatchTo(message)
}

// ReceiveCall 是业务入口(同步请求):按消息类型反射分派到业务方法。
func (e *EntityActor) ReceiveCall(_ gen.PID, _ gen.Ref, message any) (any, error) {
	return e.dispatchTo(message)
}

// dispatcher 由业务实现以接管消息分派(如加限流、埋点)。
type dispatcher interface {
	Dispatch(message any) (any, error)
}

// dispatchTo 把消息交给分派目标:业务覆写了 Dispatch 就用业务的,
// 否则用按消息类型反射分派的默认实现。
func (e *EntityActor) dispatchTo(message any) (any, error) {
	if d, ok := e.biz.(dispatcher); ok {
		return d.Dispatch(message)
	}
	return e.DispatchDefault(message)
}

// DispatchDefault 是按消息类型反射分派的默认实现。
// 业务覆写 Dispatch 时应在其中调用本方法,以免递归。
func (e *EntityActor) DispatchDefault(message any) (any, error) {
	return e.msgHandler.CallWithMsg(e.Ctx, message)
}

// AddMsgHandler 为指定对象注册消息分派,返回注册到的方法。
func (e *EntityActor) AddMsgHandler(handler any, prefix ...string) []*gxyutil.MethodMeta {
	return e.msgHandler.AddHandler(handler, prefix...)
}

// Init 是运行时回调。基类在此取得所有权。
//
// 所有权必须在同步初始化段获取(见 invariants #3):此时尚未处理任何业务
// 消息,也就不可能带着未确认的归属去服务请求。
//
// 业务覆写时应先调用本方法(取得归属),再做自己的初始化。
func (e *EntityActor) Init(args ...any) error {
	// 标识由激活协调层作为首个参数传入,类型随能力而定(role 用整数,其余用字符串)。
	if id, ok := actorIDFromArgs(args); ok {
		e.ownedID = id
	}
	if _, err := e.acquireOwnership(); err != nil {
		return err
	}
	// 分派目标在基类初始化前注册:分派在此层,业务覆写 Init 时会先调回本方法。
	if e.biz != nil {
		e.msgHandler.AddHandler(e.biz)
	}
	return e.Actor.Init(args...)
}

// Terminate 是运行时回调。基类在此释放所有权。
// 业务覆写时应先完成最终落盘,再调用本方法——顺序不可颠倒(见 invariants #4)。
func (e *EntityActor) Terminate(reason error) {
	e.dropOwnership(e.Ctx)
	e.Actor.Terminate(reason)
}

// Owner 返回本次激活持有的所有权,供业务记录(如数据库 fencing)。
func (e *EntityActor) Owner() ActorOwner {
	return e.owner
}

// ActorID 返回本实体的标识(初始化时由协调层传入)。
func (e *EntityActor) ActorID() string {
	return e.ownedID
}

func (e *EntityActor) acquireOwnership() (ActorOwner, error) {
	if e.ownedID == "" {
		return ActorOwner{}, nil
	}
	owner, err := claimOwnership(e.kind, e.ownedID)
	if err != nil {
		return ActorOwner{}, errors.Wrapf(err, "claim ownership for %s/%s", e.kind, e.ownedID)
	}
	e.owner = owner
	return owner, nil
}

func (e *EntityActor) dropOwnership(ctx context.Context) {
	if e.ownedID == "" || e.owner.NodeID == "" {
		return
	}
	if err := releaseOwnership(ctx, e.kind, e.ownedID, e.owner); err != nil {
		gxylog.Warn(ctx, "release actor ownership failed",
			gxylog.Str("kind", e.kind), gxylog.Str("id", e.ownedID), gxylog.Err(err))
	}
}

// claimOwnership / releaseOwnership 是可替换函数变量:测试注入以隔离 Redis
// (编译期安全,非 gomonkey)。
var (
	claimOwnership = func(kind, id string) (ActorOwner, error) {
		if app == nil || app.activator == nil || app.activator.locator == nil {
			return ActorOwner{}, errors.New("actor ownership is not available")
		}
		return app.activator.claim(kind, id)
	}
	releaseOwnership = func(ctx context.Context, kind, id string, owner ActorOwner) error {
		if app == nil || app.activator == nil || app.activator.locator == nil {
			return errors.New("actor ownership is not available")
		}
		_, err := app.activator.release(ctx, kind, id, owner)
		return err
	}
)

// SetOwnershipHooks 替换所有权获取/释放的实现,供测试隔离 Redis。
// 返回值用于恢复原实现。
func SetOwnershipHooks(
	claim func(kind, id string) (ActorOwner, error),
	release func(kind, id string, owner ActorOwner) error,
) func() {
	prevClaim, prevRelease := claimOwnership, releaseOwnership
	claimOwnership = claim
	releaseOwnership = func(_ context.Context, k, i string, o ActorOwner) error {
		return release(k, i, o)
	}
	return func() { claimOwnership, releaseOwnership = prevClaim, prevRelease }
}

// actorIDFromArgs 从初始化参数中取出实体标识。
// 标识由激活协调层作为首个参数传入,类型随能力而定。
func actorIDFromArgs(args []any) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	switch id := args[0].(type) {
	case string:
		return id, id != ""
	case int64:
		return strconv.FormatInt(id, 10), true
	case int:
		return strconv.Itoa(id), true
	default:
		return "", false
	}
}
