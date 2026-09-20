package gxyactor

import (
	"context"
	"errors"
	"testing"

	"ergo.services/ergo/gen"

	"gserver/core/gxyregistery"
)

type activationTestLookup struct {
	candidate *gxyregistery.ServiceInfo
}

func (l *activationTestLookup) GetServiceInfo(context.Context, string, string, gxyregistery.ServiceSelector) *gxyregistery.ServiceInfo {
	return l.candidate
}

// TestGetActorRetryRelocatesOfflineActor 陈旧记录指向其他节点时,重定位到候选节点。
// 第一次请求收到"需重试",第二次请求以允许创建的方式再次发起。
func TestGetActorRetryRelocatesOfflineActor(t *testing.T) {
	ownerLocator, callerLocator, _ := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := ownerLocator.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	owner, acquired, err := ownerLocator.Claim(ctx, "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("claim owner=%+v acquired=%v err=%v", owner, acquired, err)
	}

	mgr := NewActivatorManager("node-b", "node-b")
	mgr.store = callerLocator
	mgr.serviceLookup = &activationTestLookup{
		candidate: gxyregistery.NewServiceInfo("role", "node-c", "node-c:1002", "test", 1),
	}
	var calls []bool
	mgr.requestActorFunc = func(ctx context.Context, node string, k actorKey, allowSpawn bool) (PID, bool, error) {
		calls = append(calls, allowSpawn)
		if !allowSpawn {
			// 旧持有者已不在:释放陈旧记录,让调用方重新定位。
			_, err := ownerLocator.Release(ctx, k.kind, k.id, owner)
			return PID{}, true, err
		}
		return k.remoteRef(node), false, nil
	}

	pid, err := mgr.getActor(ctx, actorKey{kind: "role", id: "player-1"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if pid.Node() != "node-c" {
		t.Fatalf("pid node = %q, want node-c", pid.Node())
	}
	if pid.Name() != "role/player-1" {
		t.Fatalf("pid name = %q, want role/player-1", pid.Name())
	}
	if len(calls) != 2 || calls[0] || !calls[1] {
		t.Fatalf("allowSpawn calls = %v, want [false true]", calls)
	}
}

// TestGetActorWithoutSpawnReturnsNotFoundAfterStaleCleanup 只读查询遇到陈旧记录时
// 清理并重试,最终返回未找到——不得因为清理而误创建实例。
func TestGetActorWithoutSpawnReturnsNotFoundAfterStaleCleanup(t *testing.T) {
	ownerLocator, callerLocator, _ := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := ownerLocator.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	owner, acquired, err := ownerLocator.Claim(ctx, "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("claim owner=%+v acquired=%v err=%v", owner, acquired, err)
	}

	mgr := NewActivatorManager("node-b", "node-b")
	mgr.store = callerLocator
	mgr.serviceLookup = &activationTestLookup{}
	requests := 0
	mgr.requestActorFunc = func(ctx context.Context, node string, k actorKey, allowSpawn bool) (PID, bool, error) {
		requests++
		if allowSpawn {
			t.Fatal("spawn request sent for spawn=false lookup")
		}
		_, err := ownerLocator.Release(ctx, k.kind, k.id, owner)
		return PID{}, true, err
	}

	if _, err := mgr.getActor(ctx, actorKey{kind: "role", id: "player-1"}, false); err == nil {
		t.Fatal("getActor returned nil error for missing actor")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

// TestGetActorDoesNotStealWhenOwnerUnreachable 所有权在其他节点时,
// 即使请求该节点失败也不得在本地创建实例——那会产生第二个 writer。
func TestGetActorDoesNotStealWhenOwnerUnreachable(t *testing.T) {
	ownerLocator, callerLocator, _ := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := ownerLocator.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	owner, acquired, err := ownerLocator.Claim(ctx, "role", "player-1")
	if err != nil || !acquired {
		t.Fatalf("claim owner=%+v acquired=%v err=%v", owner, acquired, err)
	}

	mgr := NewActivatorManager("node-b", "node-b")
	mgr.store = callerLocator
	mgr.serviceLookup = &activationTestLookup{
		candidate: gxyregistery.NewServiceInfo("role", "node-c", "node-c:1002", "test", 1),
	}
	var askedNode string
	mgr.requestActorFunc = func(_ context.Context, node string, k actorKey, allowSpawn bool) (PID, bool, error) {
		if allowSpawn {
			t.Fatal("must not request a spawn while another node owns the actor")
		}
		askedNode = node
		return PID{}, false, errors.New("node unreachable")
	}

	if _, err := mgr.getActor(ctx, actorKey{kind: "role", id: "player-1"}, true); err == nil {
		t.Fatal("getActor must fail when the owner node is unreachable")
	}
	// 询问的必须是所有者节点,而不是重新选出的候选节点。
	if askedNode != owner.NodeID {
		t.Fatalf("asked node = %q, want owner %q", askedNode, owner.NodeID)
	}
	got, err := callerLocator.Locate(ctx, "role", "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != owner {
		t.Fatalf("ownership must be unchanged, got %+v want %+v", got, owner)
	}
}

// TestGetActorRetryIsBounded 重定位重试次数有上限,不会无界循环。
func TestGetActorRetryIsBounded(t *testing.T) {
	ownerLocator, callerLocator, _ := newActorLocatorTestPair(t)
	ctx := context.Background()
	if err := ownerLocator.acquireNodeLease(ctx); err != nil {
		t.Fatal(err)
	}
	if _, acquired, err := ownerLocator.Claim(ctx, "role", "player-1"); err != nil || !acquired {
		t.Fatalf("claim acquired=%v err=%v", acquired, err)
	}

	mgr := NewActivatorManager("node-b", "node-b")
	mgr.store = callerLocator
	mgr.serviceLookup = &activationTestLookup{}
	requests := 0
	mgr.requestActorFunc = func(context.Context, string, actorKey, bool) (PID, bool, error) {
		requests++
		return PID{}, true, nil
	}

	if _, err := mgr.getActor(ctx, actorKey{kind: "role", id: "player-1"}, true); !errors.Is(err, errActorLocateRetryExhausted) {
		t.Fatalf("getActor error = %v, want retry exhausted", err)
	}
	if requests != actorLocateMaxAttempts {
		t.Fatalf("requests = %d, want %d", requests, actorLocateMaxAttempts)
	}
}

// TestRemoteRefUsesNameAddressing 跨节点引用按"节点 + 注册名"构造。
// 运行时进程标识跨节点会被代际校验,对端重启后即失效;注册名是稳定身份。
func TestRemoteRefUsesNameAddressing(t *testing.T) {
	pid := actorKey{kind: "role", id: "1001"}.remoteRef("node-b")

	if pid.Node() != "node-b" {
		t.Fatalf("node = %q, want node-b", pid.Node())
	}
	if pid.Name() != "role/1001" {
		t.Fatalf("name = %q, want role/1001", pid.Name())
	}
	if pid.local.Node != "" {
		t.Fatal("remote ref must not carry a local runtime pid")
	}
}

// TestActorNameIsKindScoped 注册名带 kind 前缀,不同 kind 的同 id 不冲突。
func TestActorNameIsKindScoped(t *testing.T) {
	role := actorKey{kind: "role", id: "1"}
	guild := actorKey{kind: "guild", id: "1"}
	if role.name() == guild.name() {
		t.Fatal("actor name must be scoped by kind")
	}
	if role.locateKey() == guild.locateKey() {
		t.Fatal("ownership key must be scoped by kind")
	}
}

var _ = gen.PID{}
