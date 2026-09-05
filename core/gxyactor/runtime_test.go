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

type testRuntime struct {
	sent any
}

func (r *testRuntime) RegisterActorKind(string, ActorProducer) error { return nil }
func (r *testRuntime) ActivateActor(context.Context, string, string, bool) (PID, error) {
	return PID{}, nil
}
func (r *testRuntime) GetLocalActor(string, string) PID { return PID{} }
func (r *testRuntime) GetLocalActorAll(string) []PID { return nil }
func (r *testRuntime) Send(_ context.Context, _ PID, message any) error {
	r.sent = message
	return nil
}
func (r *testRuntime) Call(_ context.Context, _ PID, message any, _ time.Duration) (any, error) {
	r.sent = message
	return struct{}{}, nil
}
func (r *testRuntime) Respond(context.Context, Request, any, error) error { return nil }
func (r *testRuntime) Stop(PID) error { return nil }

func TestBusinessActorContractHasNoRuntimeActorRequirement(t *testing.T) {
	var _ ActorProducer = func() IActor { return &contractActor{} }
}

type contractActor struct{}

func (*contractActor) Init(context.Context, []any) error { return nil }
func (*contractActor) DelayInit(context.Context) error { return nil }
func (*contractActor) Terminate(context.Context, error) {}
func (*contractActor) Timer() *ActorTimer { return nil }
func (*contractActor) Self() PID { return PID{} }
func (*contractActor) HandleMessage(context.Context, any) error { return nil }
