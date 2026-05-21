package gxyactor

import (
	"context"
	"os"
	"testing"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/asynkron/protoactor-go/remote"
	"go.opentelemetry.io/otel/propagation"

	"gserver/core/gxyredis"
	"gserver/protocol/pb"
)

// ========== ActorMgr ==========

func TestActorMgr_AddAndGet(t *testing.T) {
	mgr := NewActorMgr("test")
	pid := actor.NewPID("node1", "actor1")
	mgr.Add("id1", pid)
	got := mgr.Get("id1")
	if !PidEqual(got, pid) {
		t.Fatalf("expected %v, got %v", pid, got)
	}
}

func TestActorMgr_GetNotFound(t *testing.T) {
	mgr := NewActorMgr("test")
	if got := mgr.Get("missing"); got != nil {
		t.Fatal("expected nil for missing key")
	}
}

func TestActorMgr_Remove(t *testing.T) {
	mgr := NewActorMgr("test")
	pid := actor.NewPID("node1", "actor1")
	mgr.Add("id1", pid)
	mgr.Remove("id1")
	if got := mgr.Get("id1"); got != nil {
		t.Fatal("expected nil after remove")
	}
}

func TestActorMgr_Count(t *testing.T) {
	mgr := NewActorMgr("test")
	if mgr.Count() != 0 {
		t.Fatalf("expected 0, got %d", mgr.Count())
	}
	mgr.Add("a", actor.NewPID("n", "a"))
	mgr.Add("b", actor.NewPID("n", "b"))
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
	p1 := actor.NewPID("n", "a")
	p2 := actor.NewPID("n", "b")
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
	old := actor.NewPID("n1", "a")
	newPid := actor.NewPID("n2", "a2")
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
	pid := actor.NewPID("local", "role-1")
	mgr.activatorMetas["role"] = &activatorMeta{
		Kind: "role",
		mgr:  NewActorMgr("role"),
	}
	mgr.activatorMetas["role"].mgr.Add("1", pid)
	if got := mgr.GetLocalActor("role", "1"); !PidEqual(got, pid) {
		t.Fatalf("expected %v, got %v", pid, got)
	}
	if got := mgr.GetLocalActor("role", "2"); got != nil {
		t.Fatalf("expected nil for missing actor, got %v", got)
	}
	if got := mgr.GetLocalActor("missing", "1"); got != nil {
		t.Fatalf("expected nil for missing kind, got %v", got)
	}
}

// ========== PidEqual ==========

func TestPidEqual_Same(t *testing.T) {
	a := actor.NewPID("host", "id1")
	b := actor.NewPID("host", "id1")
	if !PidEqual(a, b) {
		t.Fatal("expected equal")
	}
}

func TestPidEqual_DifferentId(t *testing.T) {
	a := actor.NewPID("host", "id1")
	b := actor.NewPID("host", "id2")
	if PidEqual(a, b) {
		t.Fatal("expected not equal")
	}
}

func TestPidEqual_DifferentHost(t *testing.T) {
	a := actor.NewPID("host1", "id1")
	b := actor.NewPID("host2", "id1")
	if PidEqual(a, b) {
		t.Fatal("expected not equal")
	}
}

func TestPidEqual_NilA(t *testing.T) {
	if PidEqual(nil, actor.NewPID("h", "i")) {
		t.Fatal("expected not equal with nil a")
	}
}

func TestPidEqual_NilB(t *testing.T) {
	if PidEqual(actor.NewPID("h", "i"), nil) {
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
	act := NewActorActivator("role", mgr)
	act.ctx = context.Background()
	key := getActorLocateKey("role", "player-1")

	if err := act.registerActorLocate(context.Background(), key); err != nil {
		t.Fatalf("registerActorLocate() error = %v", err)
	}
	t.Cleanup(func() {
		gxyredis.Redis().Del(context.Background(), key)
	})

	got, err := gxyredis.Redis().Get(context.Background(), key).Result()
	if err != nil {
		t.Fatalf("redis get key %s error = %v", key, err)
	}
	if got != mgr.nodeInstanceName {
		t.Fatalf("redis value = %q, want %q", got, mgr.nodeInstanceName)
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

	key := getActorLocateKey("role", "player-1")
	if err := gxyredis.Redis().Set(context.Background(), key, "role@node-1", ActorLocateTTL).Err(); err != nil {
		t.Fatalf("setup redis locate key error = %v", err)
	}
	t.Cleanup(func() {
		gxyredis.Redis().Del(context.Background(), key)
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
	err := ActorError("something failed")
	if err == nil {
		t.Fatal("expected non-nil")
	}
	if err.Reason != "something failed" {
		t.Fatalf("expected 'something failed', got %s", err.Reason)
	}
}

// ========== hashableActorActive ==========

func TestHashableActorActive_Hash(t *testing.T) {
	inner := &pb.ActorActive{Kind: "role", Id: "player_42"}
	h := &hashableActorActive{ActorActive: inner, hash: "player_42"}
	if h.Hash() != "player_42" {
		t.Fatalf("expected player_42, got %s", h.Hash())
	}
}

// ========== readonlyHeaderCarrier ==========

func TestReadonlyHeaderCarrier_Get(t *testing.T) {
	c := readonlyHeaderCarrier{mp: map[string]string{"trace-id": "abc123"}}
	if got := c.Get("trace-id"); got != "abc123" {
		t.Fatalf("expected abc123, got %s", got)
	}
}

func TestReadonlyHeaderCarrier_GetMissing(t *testing.T) {
	c := readonlyHeaderCarrier{mp: map[string]string{}}
	if got := c.Get("missing"); got != "" {
		t.Fatalf("expected empty, got %s", got)
	}
}

func TestReadonlyHeaderCarrier_Set(t *testing.T) {
	mp := map[string]string{}
	c := readonlyHeaderCarrier{mp: mp}
	c.Set("key", "val")
	if mp["key"] != "val" {
		t.Fatalf("expected val, got %s", mp["key"])
	}
}

func TestReadonlyHeaderCarrier_Keys(t *testing.T) {
	mp := map[string]string{"a": "1", "b": "2"}
	c := readonlyHeaderCarrier{mp: mp}
	keys := c.Keys()
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestReadonlyHeaderCarrier_KeysEmpty(t *testing.T) {
	c := readonlyHeaderCarrier{mp: map[string]string{}}
	keys := c.Keys()
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(keys))
	}
}

// ========== messageEnvelopeCarrier ==========

func TestMessageEnvelopeCarrier_GetWithHeader(t *testing.T) {
	env := &actor.MessageEnvelope{}
	env.SetHeader("k", "v")
	c := messageEnvelopeCarrier{envelope: env}
	if got := c.Get("k"); got != "v" {
		t.Fatalf("expected v, got %s", got)
	}
}

func TestMessageEnvelopeCarrier_GetNoHeader(t *testing.T) {
	c := messageEnvelopeCarrier{envelope: &actor.MessageEnvelope{}}
	if got := c.Get("k"); got != "" {
		t.Fatalf("expected empty, got %s", got)
	}
}

func TestMessageEnvelopeCarrier_GetNilEnvelope(t *testing.T) {
	c := messageEnvelopeCarrier{}
	if got := c.Get("k"); got != "" {
		t.Fatalf("expected empty, got %s", got)
	}
}

func TestMessageEnvelopeCarrier_Set(t *testing.T) {
	env := &actor.MessageEnvelope{}
	c := messageEnvelopeCarrier{envelope: env}
	c.Set("k", "v")
	if env.Header.Get("k") != "v" {
		t.Fatal("expected header to be set")
	}
}

func TestMessageEnvelopeCarrier_Keys(t *testing.T) {
	env := &actor.MessageEnvelope{}
	env.SetHeader("a", "1")
	env.SetHeader("b", "2")
	c := messageEnvelopeCarrier{envelope: env}
	keys := c.Keys()
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestMessageEnvelopeCarrier_KeysNilHeader(t *testing.T) {
	c := messageEnvelopeCarrier{envelope: &actor.MessageEnvelope{}}
	if keys := c.Keys(); keys != nil {
		t.Fatalf("expected nil, got %v", keys)
	}
}

// ========== injectTrace ==========

func TestInjectTrace_NoSpan(t *testing.T) {
	ctx := context.Background()
	result := injectTrace(ctx, &pb.ReqGuildInfo{})
	if result != nil {
		t.Fatal("expected nil when no span in context")
	}
}

func TestInjectTrace_WithEnvelope(t *testing.T) {
	// Without a valid span, injectTrace returns nil even with envelope
	ctx := context.Background()
	env := &actor.MessageEnvelope{Message: "test"}
	result := injectTrace(ctx, env)
	if result != nil {
		t.Fatal("expected nil when no span in context")
	}
}

// ========== ContextDecorator ==========

func TestContextDecorator_WrapsArgs(t *testing.T) {
	decorator := ContextDecorator("arg1", 42)
	next := func(ctx actor.Context) actor.Context {
		t.Fatal("next should not be called by this decorator")
		return ctx
	}
	wrapped := decorator(next)
	result := wrapped(nil)
	actx, ok := result.(*ActorContext)
	if !ok {
		t.Fatal("expected *ActorContext")
	}
	if actx.Context != nil {
		t.Fatal("expected original ctx to be preserved")
	}
	if len(actx.InitArgs) != 2 || actx.InitArgs[0] != "arg1" || actx.InitArgs[1] != 42 {
		t.Fatalf("unexpected args: %v", actx.InitArgs)
	}
}

// ========== messageEnvelopeCarrier implements propagation.TextMapCarrier ==========

var _ propagation.TextMapCarrier = messageEnvelopeCarrier{}

// ========== readonlyHeaderCarrier implements propagation.TextMapCarrier ==========

var _ propagation.TextMapCarrier = readonlyHeaderCarrier{}

// ========== activatorRouter pool management ==========

func TestActivatorRouter_RegisterGetPool(t *testing.T) {
	r := &activatorRouter{}
	pid := actor.NewPID("n", "pool1")
	r.RegisterPool("role", pid)
	got := r.GetPool("role")
	if !PidEqual(got, pid) {
		t.Fatalf("expected pool pid, got %v", got)
	}
}

func TestActivatorRouter_GetPoolNotFound(t *testing.T) {
	r := &activatorRouter{}
	if got := r.GetPool("missing"); got != nil {
		t.Fatal("expected nil for unregistered kind")
	}
}

func TestActivatorRouter_UnRegisterPool(t *testing.T) {
	r := &activatorRouter{}
	pid := actor.NewPID("n", "pool1")
	r.RegisterPool("role", pid)
	r.UnRegisterPool("role")
	if got := r.GetPool("role"); got != nil {
		t.Fatal("expected nil after unregister")
	}
}

func TestActivatorRouter_MultipleKinds(t *testing.T) {
	r := &activatorRouter{}
	p1 := actor.NewPID("n", "pool1")
	p2 := actor.NewPID("n", "pool2")
	r.RegisterPool("role", p1)
	r.RegisterPool("guild", p2)
	if !PidEqual(r.GetPool("role"), p1) {
		t.Fatal("expected role pool")
	}
	if !PidEqual(r.GetPool("guild"), p2) {
		t.Fatal("expected guild pool")
	}
}

func TestActivatorRouter_RegisterOverwrite(t *testing.T) {
	r := &activatorRouter{}
	p1 := actor.NewPID("n", "pool1")
	p2 := actor.NewPID("n", "pool2")
	r.RegisterPool("role", p1)
	r.RegisterPool("role", p2)
	// First match wins
	got := r.GetPool("role")
	if !PidEqual(got, p1) {
		t.Fatal("expected first registered pool")
	}
}

// ========== remote.ActorPidResponse ==========

func TestActorPidResponse_Unwrap(t *testing.T) {
	pid := actor.NewPID("host", "id")
	rsp := &remote.ActorPidResponse{Pid: pid}
	if !PidEqual(rsp.Pid, pid) {
		t.Fatal("pid mismatch")
	}
}
