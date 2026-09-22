// Package gxyactortest 提供在单元测试中创建真实 actor 的支持。
//
// 测试里的 actor 应当是绑定到运行时进程的真实实例,而不是手工拼装的半成品——
// 后者会迫使生产代码为"未初始化"的调用方留出分支。
package gxyactortest

import (
	"context"
	"testing"

	"gserver/core/gxyactor"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
)

// Spawn 在 mock 节点上创建并初始化一个 actor,返回其业务对象。
//
// 与生产路径一致:交给运行时的是门面的适配对象,业务对象藏在它里面。
// 返回的业务对象已绑定进程,可直接调用其业务方法或经 Subject 收发消息。
func Spawn[T gxyactor.Business](t testing.TB, kind string, ctor func() T, args ...any) (T, *unit.Subject) {
	t.Helper()
	var biz T
	subj, err := unit.Spawn(t, factoryFor(kind, ctor, &biz), gen.ProcessOptions{}, args...)
	if err != nil {
		var zero T
		t.Fatalf("spawn test actor: %v", err)
		return zero, nil
	}
	return biz, subj
}

// SpawnErr 在 mock 节点上创建 actor,返回初始化错误(若有)。
// 用于验证初始化失败路径。
func SpawnErr[T gxyactor.Business](t testing.TB, kind string, ctor func() T, args ...any) (T, error) {
	t.Helper()
	var biz T
	subj, err := unit.Spawn(t, factoryFor(kind, ctor, &biz), gen.ProcessOptions{}, args...)
	if err != nil {
		var zero T
		return zero, err
	}
	_ = subj
	return biz, nil
}

// factoryFor 构造进程工厂,并把创建出来的业务对象写回 out——
// 门面持有业务对象,测试需要拿到它来断言。
func factoryFor[T gxyactor.Business](kind string, ctor func() T, out *T) gen.ProcessFactory {
	return gxyactor.ActorFactory(kind, func() gxyactor.Business {
		*out = ctor()
		return *out
	})
}

// last 保存最近一次安装的所有权桩,供断言使用。
var last *Ownership

// LastOwnership 返回最近一次 StubOwnership 安装的桩。
func LastOwnership() *Ownership { return last }

// StubOwnership 把所有权载体换成内存实现,使测试不必依赖 Redis。
// 返回的 own 记录本次分配的归属,供断言使用。
func StubOwnership(t testing.TB) *Ownership {
	t.Helper()
	o := &Ownership{owners: make(map[string]gxyactor.ActorOwner)}
	last = o
	t.Cleanup(gxyactor.SetOwnershipStore(o))
	return o
}

// Ownership 是所有权载体的内存实现。
type Ownership struct {
	claimed  []string
	released []string
	epoch    uint64
	owners   map[string]gxyactor.ActorOwner

	// FailClaim 非空时,取得归属返回该错误。
	FailClaim error
}

func (o *Ownership) Claim(_ context.Context, kind, id string) (gxyactor.ActorOwner, error) {
	if o.FailClaim != nil {
		return gxyactor.ActorOwner{}, o.FailClaim
	}
	o.epoch++
	owner := gxyactor.ActorOwner{NodeID: "test@localhost", Epoch: o.epoch}
	o.claimed = append(o.claimed, kind+"/"+id)
	o.owners[kind+"/"+id] = owner
	return owner, nil
}

func (o *Ownership) Locate(_ context.Context, kind, id string) (gxyactor.ActorOwner, error) {
	return o.owners[kind+"/"+id], nil
}

func (o *Ownership) Release(_ context.Context, kind, id string, owner gxyactor.ActorOwner) (bool, error) {
	if cur, ok := o.owners[kind+"/"+id]; !ok || cur != owner {
		return false, nil
	}
	delete(o.owners, kind+"/"+id)
	o.released = append(o.released, kind+"/"+id)
	return true, nil
}

// Claimed 返回已获取所有权的标识列表。
func (o *Ownership) Claimed() []string { return o.claimed }

// Released 返回已释放所有权的标识列表。
func (o *Ownership) Released() []string { return o.released }
