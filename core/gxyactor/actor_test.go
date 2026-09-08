package gxyactor

import (
	"context"
	"os"
	"testing"

	"gserver/core/gxyredis"
)

// ========== ActorMgr ==========

func TestActorMgr_AddAndGet(t *testing.T) {
	mgr := NewActorMgr("test")
	pid := PID{Runtime: "test", Node: "node1", ID: "actor1", Creation: "1"}
	mgr.Add("id1", pid)
	got := mgr.Get("id1")
	if !PidEqual(got, pid) {
		t.Fatalf("expected %v, got %v", pid, got)
	}
}

func TestActorMgr_GetNotFound(t *testing.T) {
	mgr := NewActorMgr("test")
	if got := mgr.Get("missing"); !got.IsZero() {
		t.Fatal("expected zero PID for missing key")
	}
}

func TestActorMgr_Remove(t *testing.T) {
	mgr := NewActorMgr("test")
	pid := PID{Runtime: "test", Node: "node1", ID: "actor1", Creation: "1"}
	mgr.Add("id1", pid)
	mgr.Remove("id1")
	if got := mgr.Get("id1"); !got.IsZero() {
		t.Fatal("expected zero PID after remove")
	}
}

func TestActorMgr_Count(t *testing.T) {
	mgr := NewActorMgr("test")
	if mgr.Count() != 0 {
		t.Fatalf("expected 0, got %d", mgr.Count())
	}
	mgr.Add("a", PID{Runtime: "test", Node: "n", ID: "a", Creation: "1"})
	mgr.Add("b", PID{Runtime: "test", Node: "n", ID: "b", Creation: "1"})
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
	p1 := PID{Runtime: "test", Node: "n", ID: "a", Creation: "1"}
	p2 := PID{Runtime: "test", Node: "n", ID: "b", Creation: "1"}
	mgr.Add("a", p1)
	mgr.Add("b", p2)
	all := mgr.All()
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
}

func TestActorMgr_AllEmpty(t *testing.T) {
	mgr := NewActorMgr("test")
	all := mgr.All()
	if len(all) != 0 {
		t.Fatalf("expected 0, got %d", len(all))
	}
}

func TestActorMgr_Overwrite(t *testing.T) {
	mgr := NewActorMgr("test")
	old := PID{Runtime: "test", Node: "n1", ID: "a", Creation: "1"}
	newPid := PID{Runtime: "test", Node: "n2", ID: "a2", Creation: "2"}
	mgr.Add("id", old)
	mgr.Add("id", newPid)
	if mgr.Count() != 1 {
		t.Fatalf("expected 1, got %d", mgr.Count())
	}
	if !PidEqual(mgr.Get("id"), newPid) {
		t.Fatal("expected overwritten pid")
	}
}

func TestActivatorManager_GetLocalActor(t *testing.T) {
	mgr := NewActivatorManager("node", "node@1")
	pid := PID{Runtime: "test", Node: "local", ID: "role-1", Creation: "1"}
	mgr.activatorMetas["role"] = &activatorMeta{
		Kind: "role",
		mgr:  NewActorMgr("role"),
	}
	mgr.activatorMetas["role"].mgr.Add("1", pid)
	if got := mgr.GetLocalActor("role", "1"); !PidEqual(got, pid) {
		t.Fatalf("expected %v, got %v", pid, got)
	}
	if got := mgr.GetLocalActor("role", "2"); !got.IsZero() {
		t.Fatalf("expected zero PID for missing actor, got %v", got)
	}
	if got := mgr.GetLocalActor("missing", "1"); !got.IsZero() {
		t.Fatalf("expected zero PID for missing kind, got %v", got)
	}
}

// ========== PidEqual ==========

func TestPidEqual_Same(t *testing.T) {
	a := PID{Runtime: "test", Node: "host", ID: "id1", Creation: "1"}
	b := PID{Runtime: "test", Node: "host", ID: "id1", Creation: "1"}
	if !PidEqual(a, b) {
		t.Fatal("expected equal")
	}
}

func TestPidEqual_DifferentId(t *testing.T) {
	a := PID{Runtime: "test", Node: "host", ID: "id1", Creation: "1"}
	b := PID{Runtime: "test", Node: "host", ID: "id2", Creation: "1"}
	if PidEqual(a, b) {
		t.Fatal("expected not equal")
	}
}

func TestPidEqual_DifferentHost(t *testing.T) {
	a := PID{Runtime: "test", Node: "host1", ID: "id1", Creation: "1"}
	b := PID{Runtime: "test", Node: "host2", ID: "id1", Creation: "1"}
	if PidEqual(a, b) {
		t.Fatal("expected not equal")
	}
}

func TestPidEqual_NilA(t *testing.T) {
	if PidEqual(PID{}, PID{Runtime: "test", Node: "h", ID: "i", Creation: "1"}) {
		t.Fatal("expected not equal with nil a")
	}
}

func TestPidEqual_NilB(t *testing.T) {
	if PidEqual(PID{Runtime: "test", Node: "h", ID: "i", Creation: "1"}, PID{}) {
		t.Fatal("expected not equal with nil b")
	}
}

func TestPidEqual_BothNil(t *testing.T) {
	if PidEqual(nil, nil) {
		t.Fatal("expected not equal with both nil")
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

func TestRegisterActorLocate(t *testing.T) {
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

func TestGetActorLocateNodeName(t *testing.T) {
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

	ownerKey := getActorLocateKey("role", "player-1")
	leaseKey := actorLocatorLeaseKey("role@node-1")
	if err := gxyredis.Redis().Set(context.Background(), ownerKey, "role@node-1|1|test-token", 0).Err(); err != nil {
		t.Fatalf("setup redis locate key error = %v", err)
	}
	if err := gxyredis.Redis().Set(context.Background(), leaseKey, "test-token", actorLocateLeaseTTL).Err(); err != nil {
		t.Fatalf("setup redis lease key error = %v", err)
	}
	t.Cleanup(func() {
		gxyredis.Redis().Del(context.Background(), ownerKey, leaseKey)
	})

	got, err := getActorLocateNodeName(context.Background(), "role", "player-1")
	if err != nil {
		t.Fatalf("getActorLocateNodeName() error = %v", err)
	}
	if got != "role@node-1" {
		t.Fatalf("getActorLocateNodeName() = %q, want %q", got, "role@node-1")
	}

	if _, err := getActorLocateNodeName(context.Background(), "role", "missing"); err != nil {
		t.Fatalf("getActorLocateNodeName() missing error = %v", err)
	}
}

// ========== ActorError ==========

func TestActorError(t *testing.T) {
	if got := ActorError("something failed"); got.Reason != "something failed" {
		t.Fatalf("reason = %q", got.Reason)
	}
}
