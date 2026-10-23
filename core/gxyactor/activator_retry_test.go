package gxyactor

import (
	"context"
	"strings"
	"testing"

	"gserver/core/gxyregistery"
	"gserver/core/gxyregistery/types"
	"gserver/protocol/pb"

	"ergo.services/ergo/gen"
	"github.com/cockroachdb/errors"
)

// countingStore 是可按脚本应答的 OwnershipStore,并记录 Locate/Release 次数。
type countingStore struct {
	locate    []ActorOwner // 依次返回;用尽后返回零值(无 owner)
	locateN   int
	releaseOK bool
	releaseN  int
}

func (s *countingStore) Claim(context.Context, string, string) (ActorOwner, error) {
	return ZeroActorOwner, errors.New("unused")
}

func (s *countingStore) Locate(context.Context, string, string) (ActorOwner, error) {
	s.locateN++
	if s.locateN > len(s.locate) {
		return ZeroActorOwner, nil
	}
	return s.locate[s.locateN-1], nil
}

func (s *countingStore) Release(context.Context, string, string, ActorOwner) (bool, error) {
	s.releaseN++
	return s.releaseOK, nil
}

// fixedLookup 固定返回一个候选节点(info 为 nil 表示目录里没有可用节点)。
type fixedLookup struct{ info *types.ServiceInfo }

func (l fixedLookup) GetServiceInfo(context.Context, string, string, gxyregistery.ServiceSelector) *types.ServiceInfo {
	return l.info
}

// 重试契约(见 errLocateRetry 定义):只有"这一轮没形成结论"才重试。
// 下面几条用 Locate 的调用次数把这条契约钉住 —— 改动分类时它们会红。

// TestActivateDoesNotRetryDeterministicFailure 确定性失败只尝试一次。
func TestActivateDoesNotRetryDeterministicFailure(t *testing.T) {
	const node = "n1"
	store := &countingStore{}
	mgr := &activatorManager{
		nodeID: node,
		store:  store,
		kinds:  map[string]gen.ProcessFactory{}, // kind 未登记 → 确定性失败
		serviceLookup: fixedLookup{
			info: types.NewServiceInfo("role", node, "127.0.0.1", "v", 1),
		},
	}

	_, err := mgr.activateActor(context.Background(), actorKey{kind: "role", id: "1"}, true)
	if err == nil {
		t.Fatal("未登记的 kind 必须失败")
	}
	if errors.Is(err, errLocateRetry) {
		t.Fatalf("确定性失败不该被标成需要重试: %v", err)
	}
	if store.locateN != 1 {
		t.Fatalf("Locate 调用了 %d 次, want 1(确定性失败不重试)", store.locateN)
	}
}

// TestActivateRetriesAfterReleasingStaleOwner 清理陈旧记录后必须重读记录。
func TestActivateRetriesAfterReleasingStaleOwner(t *testing.T) {
	const node = "n1"
	store := &countingStore{
		locate:    []ActorOwner{{NodeID: node, Epoch: 1}},
		releaseOK: true,
	}
	mgr := &activatorManager{
		nodeID:        node,
		store:         store,
		kinds:         map[string]gen.ProcessFactory{},
		serviceLookup: fixedLookup{}, // 第二轮目录为空 → 确定性错误,便于数轮数
	}

	_, err := mgr.activateActor(context.Background(), actorKey{kind: "role", id: "1"}, true)
	if err == nil {
		t.Fatal("目录里没有候选节点时必须失败")
	}
	if errors.Is(err, errLocateRetry) {
		t.Fatalf("最终错误不该是重试标记: %v", err)
	}
	if store.releaseN != 1 {
		t.Fatalf("Release 调用了 %d 次, want 1", store.releaseN)
	}
	if store.locateN != 2 {
		t.Fatalf("Locate 调用了 %d 次, want 2(释放陈旧记录后必须重读)", store.locateN)
	}
}

// TestActivateRetryExhaustedCarriesReason 重试耗尽必须带出最后一次的原因。
func TestActivateRetryExhaustedCarriesReason(t *testing.T) {
	const node = "n1"
	stale := ActorOwner{NodeID: node, Epoch: 1}
	store := &countingStore{
		// 三轮都命中同一条陈旧记录:每轮释放后重试,直到次数用尽。
		locate:    []ActorOwner{stale, stale, stale},
		releaseOK: true,
	}
	mgr := &activatorManager{
		nodeID:        node,
		store:         store,
		kinds:         map[string]gen.ProcessFactory{},
		serviceLookup: fixedLookup{},
	}

	_, err := mgr.activateActor(context.Background(), actorKey{kind: "role", id: "1"}, true)
	if !errors.Is(err, errActorLocateRetryExhausted) {
		t.Fatalf("err = %v, want errActorLocateRetryExhausted", err)
	}
	if !strings.Contains(err.Error(), "released stale owner") {
		t.Fatalf("重试耗尽必须带出最后一次的原因, 实际: %v", err)
	}
}

// TestHandleActiveRetriesWhenRecordDisappeared 记录刚消失时,让调用方重新定位,
// 不能回 "not found" —— 否则 ActivateRole(spawn=true) 会因一次并发释放而失败。
func TestHandleActiveRetriesWhenRecordDisappeared(t *testing.T) {
	mgr := &activatorManager{
		nodeID: "n1",
		store:  &countingStore{}, // 记录为空
		kinds:  map[string]gen.ProcessFactory{},
	}
	a := newActivatorActor(mgr)

	got, err := a.handleActive(context.Background(), &pb.ActorActive{
		Kind: "role", Id: "1", AllowSpawn: false,
	})
	if err != nil {
		t.Fatalf("记录刚消失不该报错: %v", err)
	}
	retry, ok := got.(*pb.ActorLocateRetry)
	if !ok {
		t.Fatalf("应答 = %T, want *pb.ActorLocateRetry", got)
	}
	if retry.GetReason() != "record disappeared" {
		t.Fatalf("reason = %q, want %q(原因必须随应答带回,不能靠远端日志)", retry.GetReason(), "record disappeared")
	}
}

// TestHandleActiveRetryReportsOwnerNode 记录指向别的节点时,原因里要说是谁。
func TestHandleActiveRetryReportsOwnerNode(t *testing.T) {
	mgr := &activatorManager{
		nodeID: "n1",
		store:  &countingStore{locate: []ActorOwner{{NodeID: "n2", Epoch: 7}}},
		kinds:  map[string]gen.ProcessFactory{},
	}
	a := newActivatorActor(mgr)

	got, err := a.handleActive(context.Background(), &pb.ActorActive{
		Kind: "role", Id: "1", AllowSpawn: true,
	})
	if err != nil {
		t.Fatalf("记录指向别的节点不该报错: %v", err)
	}
	retry, ok := got.(*pb.ActorLocateRetry)
	if !ok {
		t.Fatalf("应答 = %T, want *pb.ActorLocateRetry", got)
	}
	if retry.GetReason() != "record owned by n2" {
		t.Fatalf("reason = %q, want %q", retry.GetReason(), "record owned by n2")
	}
}

// TestHandleActiveReportsDeterministicSpawnFailure 远端确定性失败必须走业务错误
// 通道:调用方立刻拿到真因,而不是重试到耗尽。
func TestHandleActiveReportsDeterministicSpawnFailure(t *testing.T) {
	mgr := &activatorManager{
		nodeID: "n1",
		store:  &countingStore{},
		kinds:  map[string]gen.ProcessFactory{}, // kind 未登记
	}
	a := newActivatorActor(mgr)

	got, err := a.handleActive(context.Background(), &pb.ActorActive{
		Kind: "role", Id: "1", AllowSpawn: true,
	})
	if err == nil {
		t.Fatalf("确定性失败必须作为错误返回, 实际应答 %T", got)
	}
}
