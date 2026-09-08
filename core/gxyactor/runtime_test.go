package gxyactor

import (
	"context"
	"testing"
	"time"
)

func TestPIDEqualityUsesAllIdentityDimensions(t *testing.T) {
	base := PID{Runtime: "ergo-v1", Node: "node-a", ID: "role/42", Creation: "inc-1"}
	if !PidEqual(base, base) {
		t.Fatal("same normalized PID must compare equal")
	}
	for name, changed := range map[string]PID{
		"runtime":  {Runtime: "other", Node: base.Node, ID: base.ID, Creation: base.Creation},
		"node":     {Runtime: base.Runtime, Node: "node-b", ID: base.ID, Creation: base.Creation},
		"id":       {Runtime: base.Runtime, Node: base.Node, ID: "role/43", Creation: base.Creation},
		"creation": {Runtime: base.Runtime, Node: base.Node, ID: base.ID, Creation: "inc-2"},
	} {
		t.Run(name, func(t *testing.T) {
			if PidEqual(base, changed) {
				t.Fatalf("PID differing in %s must not compare equal", name)
			}
		})
	}
}

func TestRuntimeNeutralHelpersAcceptOpaqueMessages(t *testing.T) {
	var runtime testRuntime
	SetRuntime(&runtime)
	t.Cleanup(func() { SetRuntime(nil) })
	pid := PID{Runtime: "test", Node: "node-a", ID: "role/1", Creation: "1"}
	message := struct{ Value string }{Value: "opaque"}
	if err := Send(context.Background(), pid, message); err != nil {
		t.Fatal(err)
	}
	if _, err := Call(context.Background(), pid, message, time.Second); err != nil {
		t.Fatal(err)
	}
	if runtime.sent != message {
		t.Fatalf("sent %#v, want %#v", runtime.sent, message)
	}
}
func TestRuntimeExposesActorCreationContract(t *testing.T) {
	runtime := &testRuntime{}
	pid, err := spawnThroughRuntime(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if pid.ID != "spawned" {
		t.Fatalf("spawned PID = %#v, want ID spawned", pid)
	}
}
func TestRuntimeExposesNamedCallContract(t *testing.T) {
	runtime := &testRuntime{}
	response, err := callNamedThroughRuntime(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if response != "called" {
		t.Fatalf("named call response = %#v, want called", response)
	}
}

func callNamedThroughRuntime(runtime Runtime) (any, error) {
	return runtime.CallNamed(context.Background(), "node-a", "Activator/role", "activate", time.Second)
}

func spawnThroughRuntime(runtime Runtime) (PID, error) {
	return runtime.Spawn("test", "id", "init")
}

type testRuntime struct {
	sent       any
	localSent  any
	deregister string
}

func (r *testRuntime) Spawn(string, string, ...any) (PID, error) {
	return PID{Runtime: "test", Node: "node-a", ID: "spawned", Creation: "1"}, nil
}
func (r *testRuntime) CallNamed(context.Context, string, string, any, time.Duration) (any, error) {
	return "called", nil
}
func (r *testRuntime) SpawnNamed(string, string, ActorProducer, ...any) (PID, error) {
	return PID{Runtime: "test", Node: "node-a", ID: "named", Creation: "1"}, nil
}
func (r *testRuntime) RegisterActorKind(string, ActorProducer) error { return nil }
func (r *testRuntime) DeregisterActorKind(kind string)               { r.deregister = kind }
func (r *testRuntime) ActivateActor(context.Context, string, string, bool) (PID, error) {
	return PID{}, nil
}
func (r *testRuntime) GetLocalActor(string, string) PID                 { return PID{} }
func (r *testRuntime) GetLocalActorAll(string) []PID                    { return nil }
func (r *testRuntime) Send(_ context.Context, _ PID, message any) error { r.sent = message; return nil }
func (r *testRuntime) LocalSend(_ context.Context, _ PID, message any) error {
	r.localSent = message
	return nil
}
func (r *testRuntime) Call(_ context.Context, _ PID, message any, _ time.Duration) (any, error) {
	r.sent = message
	return struct{}{}, nil
}
func (r *testRuntime) Respond(context.Context, Request, any, error) error { return nil }
func (r *testRuntime) Stop(PID) error                                     { return nil }

func TestBusinessActorContractHasNoRuntimeActorRequirement(t *testing.T) {
	var _ ActorProducer = func() IActor { return &contractActor{} }
}

type contractActor struct{}

func (*contractActor) Init(ActorContext, []any) error        { return nil }
func (*contractActor) DelayInit(ActorContext) error          { return nil }
func (*contractActor) Terminate(ActorContext, error)         {}
func (*contractActor) HandleMessage(ActorContext, any) error { return nil }

func TestRuntimeDispatchesLocalSendAndDeregister(t *testing.T) {
	var runtime testRuntime
	SetRuntime(&runtime)
	t.Cleanup(func() { SetRuntime(nil) })
	pid := PID{Runtime: "test", Node: "node", ID: "id", Creation: "1"}
	message := struct{ Value string }{Value: "local"}
	if err := LocalSend(context.Background(), pid, message); err != nil {
		t.Fatal(err)
	}
	DeregisterActorKind("role")
	if runtime.localSent != message || runtime.deregister != "role" {
		t.Fatalf("local=%#v deregister=%q", runtime.localSent, runtime.deregister)
	}
}

func TestUninitializedHelpersReturnErrors(t *testing.T) {
	SetRuntime(nil)
	t.Cleanup(func() { SetRuntime(nil) })
	pid := PID{Runtime: "test", Node: "node", ID: "id", Creation: "1"}
	if err := Send(context.Background(), pid, struct{}{}); err == nil {
		t.Fatal("Send unexpectedly succeeded")
	}
	if _, err := Call(context.Background(), pid, struct{}{}, time.Second); err == nil {
		t.Fatal("Call unexpectedly succeeded")
	}
	if _, err := ActivateActor(context.Background(), "role", "1", true); err == nil {
		t.Fatal("ActivateActor unexpectedly succeeded")
	}
	if err := Stop(pid); err == nil {
		t.Fatal("Stop unexpectedly succeeded")
	}
}
