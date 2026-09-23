package gxyactor

import (
	"context"
	"strconv"

	"github.com/cockroachdb/errors"

	"gserver/core/gxylog"
)

// entityProvider 由内嵌 EntityActor 的业务对象满足(方法提升)。
// 必须用**接口**断言而非类型断言:业务对象是外层结构体,不是 *EntityActor。
type entityProvider interface{ entityActor() *EntityActor }

// entityActor 让内嵌本类型的业务对象被识别为承载实体。
func (e *EntityActor) entityActor() *EntityActor { return e }

// entityOf 取出业务对象内嵌的实体层;不承载实体时返回 nil。
func entityOf(biz Business) *EntityActor {
	if e, ok := biz.(entityProvider); ok {
		return e.entityActor()
	}
	return nil
}

// EntityActor 是**承载持久实体**的 actor 的业务基类:在 Actor 之上加上所有权。
//
// 为什么单独一层(见 ADR 0015):代表一个需要单写者保护的实体,就必然要声明
// 归属——这不是可选项。把所有权放在只参与运行时的 Actor 上,会让会话、激活
// 协调者这类 actor 也继承一整套用不到的机制,而且"是否承载实体"会退化成
// "创建时恰好有没有传参数"这一运行期约定。
//
// 内嵌本类型即表示:本 actor 承载实体,因此参与所有权协议。归属的取得与释放
// 由门面在初始化段与终止路径上驱动(见 ADR 0017)——业务看不到时序,也不必
// 记得调用基类,顺序因此不可能被写错(不变量 #3、#4)。
//
// 本层**只负责所有权**。按消息类型的分派属于"是个 actor"这一层,在 Actor 上;
// 两者无关:不承载实体的 actor 一样可以按消息类型分派。
type EntityActor struct {
	*Actor

	// 本次激活持有的所有权,在同步初始化段获取、终止路径释放。
	owner   ActorOwner
	ownedID string // 本实体的标识(初始化时由协调层传入)
}

// NewEntityActor 创建承载实体的业务基类。
func NewEntityActor() *EntityActor {
	return &EntityActor{Actor: NewActor()}
}

// ===== 门面内部:由运行时适配对象驱动 =====

// acquireOwnership 在同步初始化段取得归属。
//
// 标识取不到即失败:承载实体却没有标识,等于一个不受单写者保护的实体——
// 那正是本层要防的事,不能静默放过。
func (e *EntityActor) acquireOwnership() (ActorOwner, error) {
	// 标识由激活协调层作为首个参数传入,类型随能力而定(role 用整数,其余用字符串)。
	id, ok := actorIDFromArgs(e.initArgs)
	if !ok {
		return ZeroActorOwner, errors.Errorf("entity actor %q requires an id as the first init arg", e.kind)
	}
	e.ownedID = id
	if e.ownedID == "" {
		return ZeroActorOwner, nil
	}
	key := actorKey{kind: e.kind, id: e.ownedID}
	owner, err := claimOwnership(e.Ctx, key)
	if err != nil {
		return ZeroActorOwner, errors.Wrapf(err, "claim ownership for %s", key)
	}
	e.owner = owner
	return owner, nil
}

// dropOwnership 在终止路径释放归属。释放失败只记录:此时已无补救手段,
// 而残留记录由激活协调层在下次激活时条件清理(不变量 #8)。
func (e *EntityActor) dropOwnership(ctx context.Context) {
	if e.ownedID == "" || e.owner.IsZero() {
		return
	}
	key := actorKey{kind: e.kind, id: e.ownedID}
	if err := releaseOwnership(ctx, key, e.owner); err != nil {
		gxylog.Warn(ctx, "release actor ownership failed",
			gxylog.Str("actor", key.String()), gxylog.Err(err))
	}
}

// Owner 返回本次激活持有的所有权,供业务记录(如数据库 fencing)。
func (e *EntityActor) Owner() ActorOwner { return e.owner }

// ActorID 返回本实体的标识(初始化时由协调层传入)。
func (e *EntityActor) ActorID() string { return e.ownedID }

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
