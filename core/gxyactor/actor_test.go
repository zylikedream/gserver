package gxyactor

import (
	"context"
	"os"
	"testing"

	"ergo.services/ergo/gen"

	"gserver/core/gxyredis"
	"gserver/protocol/pb"
)

// ========== ActorMgr ==========
// ActorMgr 是通用的"id → 进程引用"登记表,gateway 用它跟踪在线会话。

func TestActorMgr_AddAndGet(t *testing.T) {
	mgr := NewActorMgr("test")
	pid := pidFromLocal(gen.PID{Node: "node1", ID: 1})
	mgr.Add("id1", pid)
	got := mgr.Get("id1")
	if !PidEqual(got, pid) {
		t.Fatalf("expected %v, got %v", pid, got)
	}
}

func TestActorMgr_GetNotFound(t *testing.T) {
	mgr := NewActorMgr("test")
	if got := mgr.Get("missing"); !PIDIsZero(got) {
		t.Fatalf("expected zero pid for missing key, got %v", got)
	}
}

func TestActorMgr_Remove(t *testing.T) {
	mgr := NewActorMgr("test")
	mgr.Add("id1", pidFromLocal(gen.PID{Node: "node1", ID: 1}))
	mgr.Remove("id1")
	if got := mgr.Get("id1"); !PIDIsZero(got) {
		t.Fatalf("expected zero pid after remove, got %v", got)
	}
}

func TestActorMgr_Count(t *testing.T) {
	mgr := NewActorMgr("test")
	if mgr.Count() != 0 {
		t.Fatalf("expected 0, got %d", mgr.Count())
	}
	mgr.Add("a", pidFromLocal(gen.PID{Node: "n", ID: 1}))
	mgr.Add("b", pidFromLocal(gen.PID{Node: "n", ID: 2}))
	if mgr.Count() != 2 {
		t.Fatalf("expected 2, got %d", mgr.Count())
	}
	mgr.Remove("a")
	if mgr.Count() != 1 {
		t.Fatalf("expected 1, got %d", mgr.Count())
	}
}

func TestActorMgr_All(t *testing.T) {
	mgr := NewActorMgr("test")
	mgr.Add("a", pidFromLocal(gen.PID{Node: "n", ID: 1}))
	mgr.Add("b", pidFromLocal(gen.PID{Node: "n", ID: 2}))
	if all := mgr.All(); len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
}

func TestActorMgr_AllEmpty(t *testing.T) {
	mgr := NewActorMgr("test")
	if all := mgr.All(); len(all) != 0 {
		t.Fatalf("expected 0, got %d", len(all))
	}
}

func TestActorMgr_Overwrite(t *testing.T) {
	mgr := NewActorMgr("test")
	newPid := pidFromLocal(gen.PID{Node: "n2", ID: 9})
	mgr.Add("id", pidFromLocal(gen.PID{Node: "n1", ID: 1}))
	mgr.Add("id", newPid)
	if mgr.Count() != 1 {
		t.Fatalf("expected 1, got %d", mgr.Count())
	}
	if !PidEqual(mgr.Get("id"), newPid) {
		t.Fatal("expected overwritten pid")
	}
}

// ========== PidEqual ==========
// 进程标识是值类型:节点、序号、创建时刻三者共同决定身份。

func TestPidEqual_Same(t *testing.T) {
	a := pidFromLocal(gen.PID{Node: "host", ID: 1, Creation: 100})
	b := pidFromLocal(gen.PID{Node: "host", ID: 1, Creation: 100})
	if !PidEqual(a, b) {
		t.Fatal("expected equal")
	}
}

func TestPidEqual_DifferentId(t *testing.T) {
	a := pidFromLocal(gen.PID{Node: "host", ID: 1})
	b := pidFromLocal(gen.PID{Node: "host", ID: 2})
	if PidEqual(a, b) {
		t.Fatal("expected not equal")
	}
}

func TestPidEqual_DifferentHost(t *testing.T) {
	a := pidFromLocal(gen.PID{Node: "host1", ID: 1})
	b := pidFromLocal(gen.PID{Node: "host2", ID: 1})
	if PidEqual(a, b) {
		t.Fatal("expected not equal")
	}
}

// 创建时刻不同即视为不同实例:同一节点重启后,旧引用不得命中新进程。
func TestPidEqual_DifferentCreation(t *testing.T) {
	a := pidFromLocal(gen.PID{Node: "host", ID: 1, Creation: 100})
	b := pidFromLocal(gen.PID{Node: "host", ID: 1, Creation: 200})
	if PidEqual(a, b) {
		t.Fatal("expected not equal for different creation")
	}
}

func TestPidEqual_ZeroA(t *testing.T) {
	if PidEqual(PID{}, pidFromLocal(gen.PID{Node: "h", ID: 1})) {
		t.Fatal("expected not equal with zero a")
	}
}

func TestPidEqual_ZeroB(t *testing.T) {
	if PidEqual(pidFromLocal(gen.PID{Node: "h", ID: 1}), PID{}) {
		t.Fatal("expected not equal with zero b")
	}
}

func TestPidEqual_BothZero(t *testing.T) {
	if !PidEqual(PID{}, PID{}) {
		t.Fatal("expected equal for two zero pids")
	}
}

// ========== PIDIsZero ==========

func TestPIDIsZero(t *testing.T) {
	if !PIDIsZero(PID{}) {
		t.Fatal("zero pid must be reported as zero")
	}
	if PIDIsZero(pidFromLocal(gen.PID{Node: "h", ID: 1})) {
		t.Fatal("non-zero pid must not be reported as zero")
	}
}

// ========== getActorLocateKey ==========

func TestGetActorLocateKey(t *testing.T) {
	key := getActorLocateKey("role", "123")
	expected := "gserver:locate:node:actor:role:123"
	if key != expected {
		t.Fatalf("expected %s, got %s", expected, key)
	}
}

func TestGetActorLocateKey_EmptyKind(t *testing.T) {
	key := getActorLocateKey("", "456")
	expected := "gserver:locate:node:actor::456"
	if key != expected {
		t.Fatalf("expected %s, got %s", expected, key)
	}
}

// ========== 所有权获取（需要 Redis）==========

func TestClaimAndLocate(t *testing.T) {
	if os.Getenv("RUN_REDIS_TESTS") != "1" {
		t.Skip("set RUN_REDIS_TESTS=1 to run Redis integration tests")
	}
	redisApp := gxyredis.NewRedisApp()
	if err := redisApp.OnModInit(context.Background()); err != nil {
		t.Skipf("redis test config unavailable: %v", err)
	}
	if err := redisApp.OnModStart(context.Background()); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}
	t.Cleanup(func() {
		_ = redisApp.OnModStop(context.Background())
	})

	mgr := NewActivatorManager("node", "node@1")
	if err := mgr.locator.acquireNodeLease(context.Background()); err != nil {
		t.Fatalf("acquireNodeLease() error = %v", err)
	}
	owner, acquired, err := mgr.locator.claim(context.Background(), "role", "player-1")
	if err != nil {
		t.Fatalf("claim() error = %v", err)
	}
	if !acquired {
		t.Fatal("claim() did not acquire a new owner")
	}
	t.Cleanup(func() {
		_, _ = mgr.locator.release(context.Background(), "role", "player-1", owner)
		_ = mgr.locator.releaseNodeLease(context.Background())
	})

	got, err := mgr.locator.locate(context.Background(), "role", "player-1")
	if err != nil {
		t.Fatalf("locate() error = %v", err)
	}
	if got != owner {
		t.Fatalf("owner = %+v, want %+v", got, owner)
	}
}

// ========== ActorError ==========

func TestActorError(t *testing.T) {
	err := ActorError("something failed")
	if err == nil {
		t.Fatal("expected non-nil")
	}
	if err.GetReason() != "something failed" {
		t.Fatalf("reason = %q, want %q", err.GetReason(), "something failed")
	}
}

var _ = pb.ActorError{}
