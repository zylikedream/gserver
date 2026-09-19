// Package gxyactortest 提供在单元测试中创建真实 actor 的支持。
//
// 测试里的 actor 应当是绑定到运行时进程的真实实例,而不是手工拼装的半成品——
// 后者会迫使生产代码为"未初始化"的调用方留出分支。
package gxyactortest

import (
	"testing"

	"gserver/core/gxyactor"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
)

// Spawn 在 mock 节点上创建并初始化一个 actor,返回其业务实例。
// 返回的 actor 拥有真实的进程标识与节点,可直接调用其业务方法或经 Subject 收发消息。
func Spawn[T gen.ProcessBehavior](t testing.TB, factory func() T, args ...any) (T, *unit.Subject) {
	t.Helper()
	subj, err := unit.Spawn(t, func() gen.ProcessBehavior { return factory() }, gen.ProcessOptions{}, args...)
	if err != nil {
		var zero T
		t.Fatalf("spawn test actor: %v", err)
		return zero, nil
	}
	return subj.Behavior().(T), subj
}

// SpawnErr 在 mock 节点上创建并初始化 actor,返回初始化错误(若有)。
// 用于验证初始化失败路径。
func SpawnErr[T gen.ProcessBehavior](t testing.TB, factory func() T, args ...any) (T, error) {
	t.Helper()
	subj, err := unit.Spawn(t, func() gen.ProcessBehavior { return factory() }, gen.ProcessOptions{}, args...)
	if err != nil {
		var zero T
		return zero, err
	}
	return subj.Behavior().(T), nil
}

// last 保存最近一次安装的所有权桩,供断言使用。
var last *Ownership

// LastOwnership 返回最近一次 StubOwnership 安装的桩。
func LastOwnership() *Ownership { return last }

// StubOwnership 把所有权获取/释放替换成内存实现,使测试不必依赖 Redis。
// 返回的 own 记录本次分配的所有权,供断言使用。
func StubOwnership(t testing.TB) *Ownership {
	t.Helper()
	o := &Ownership{}
	last = o
	// 每个调用点分配一个新的世代,与实际实现一样单调递增。
	restore := gxyactor.SetOwnershipHooks(o.claim, o.release)
	t.Cleanup(restore)
	return o
}

// Ownership 是所有权接缝的内存实现。
type Ownership struct {
	claimed  []string
	released []string
	epoch    uint64
	// FailClaim 非空时,获取所有权返回该错误。
	FailClaim error
}

func (o *Ownership) claim(kind, id string) (gxyactor.ActorOwner, error) {
	if o.FailClaim != nil {
		return gxyactor.ActorOwner{}, o.FailClaim
	}
	o.epoch++
	o.claimed = append(o.claimed, kind+"/"+id)
	return gxyactor.ActorOwner{NodeID: "test@localhost", Epoch: o.epoch}, nil
}

func (o *Ownership) release(kind, id string, _ gxyactor.ActorOwner) error {
	o.released = append(o.released, kind+"/"+id)
	return nil
}

// Claimed 返回已获取所有权的标识列表。
func (o *Ownership) Claimed() []string { return o.claimed }

// Released 返回已释放所有权的标识列表。
func (o *Ownership) Released() []string { return o.released }
