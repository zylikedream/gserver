package gxyactor

import (
	"context"

	"github.com/cockroachdb/errors"

	"ergo.services/ergo/gen"
)

// OwnershipStore 是所有权协议的持久化载体。
//
// 生产实现即 actorLocator(actor_locator.go):归属操作把每实例租约令牌交给 Redis
// 脚本仲裁,并以租约有效性做前置闸门,因此"谁持有"与"谁有权持有"必须出自同一份
// 判据——不由两个独立存储分别承担(见 invariants #1、#6)。
//
// 这是**依赖注入点**,不是测试专用开关:归属记录存在哪里本来就是可替换的组件
// (默认实现基于 Redis,见 actor_locator.go)。测试传入内存实现,就不必为了跑一个
// actor 而架一套 Redis——业务代码里因此不需要任何测试钩子。
//
// 接口以 kind 与 id 两个字符串给出,理由有两条:存储 API 的键词汇本来就是标量
// (实现里 Redis 键由它们派生);包外实现也因此能满足它——测试不需要为了注入一个
// 内存载体而依赖门面内部的身份类型。
type OwnershipStore interface {
	// Claim 尝试取得归属。acquired=false 表示本节点已是持有者(前驱留下的记录),
	// 由调用方按陈旧记录处理。
	Claim(ctx context.Context, kind string, id string) (ActorOwner, bool, error)

	// Locate 查询当前归属;无归属时返回零值。
	Locate(ctx context.Context, kind string, id string) (ActorOwner, error)

	// Release 条件释放:只有仍由 owner 持有时才释放。
	Release(ctx context.Context, kind string, id string, owner ActorOwner) (bool, error)
}

// ownershipStore 返回当前生效的载体。未初始化时明确失败,不静默放过——
// 放过等于让一个不受单写者保护的实体开始服务(见 invariants #3)。
func ownershipStore() (OwnershipStore, error) {
	if app == nil || app.activator == nil || app.activator.store == nil {
		return nil, errors.New("actor ownership store is not available")
	}
	return app.activator.store, nil
}

// claimOwnership 取得归属。由实体层在同步初始化段调用(ADR 0012)。
func claimOwnership(ctx context.Context, k actorKey) (ActorOwner, error) {
	store, err := ownershipStore()
	if err != nil {
		return ActorOwner{}, err
	}
	owner, acquired, err := store.Claim(ctx, k.kind, k.id)
	if err != nil {
		return ActorOwner{}, err
	}
	if !acquired {
		// 本节点已是持有者:这是前驱留下的记录,交由上层按陈旧记录处理。
		return ActorOwner{}, errors.Wrapf(ErrNotOwner, "actor %s", k)
	}
	return owner, nil
}

// releaseOwnership 释放归属。由门面在终止路径上、业务最终落库之后调用(不变量 #4)。
func releaseOwnership(ctx context.Context, k actorKey, owner ActorOwner) error {
	store, err := ownershipStore()
	if err != nil {
		return err
	}
	_, err = store.Release(ctx, k.kind, k.id, owner)
	return err
}

// SetOwnershipStore 替换所有权载体,返回用于恢复原实现的函数。
//
// 生产路径不需要调用它:载体随 actor 模块启动而建立。它存在是为了让所有权
// 载体可注入——测试因此不必依赖 Redis,也不必要求业务代码为测试留出分支。
func SetOwnershipStore(store OwnershipStore) func() {
	prev := app
	if prev == nil || prev.activator == nil {
		// 尚未启动 actor 模块:用一个只承载载体的应用替代它。租约与激活不在
		// 测试范围内,因此不复制它们的组件。
		//
		// 绝不改动已存在的那个 app:它的 activator 由启动过程填充,替它补一个
		// 会污染真实的模块状态。
		app = &actorApp{activator: &activatorManager{
			kinds:  make(map[string]gen.ProcessFactory),
			store:  store,
			nodeID: "test",
		}}
		return func() { app = prev }
	}

	prevStore := prev.activator.store
	prev.activator.store = store
	return func() { prev.activator.store = prevStore }
}
