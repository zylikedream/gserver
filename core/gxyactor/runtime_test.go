package gxyactor

import (
	"context"
	"errors"
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
	sent       any
	localSent  any
	deregister string
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

func (*contractActor) Init(context.Context, []any) error        { return nil }
func (*contractActor) DelayInit(context.Context) error          { return nil }
func (*contractActor) Terminate(context.Context, error)         {}
func (*contractActor) Timer() *ActorTimer                       { return nil }
func (*contractActor) Self() PID                                { return PID{} }
func (*contractActor) HandleMessage(context.Context, any) error { return nil }

func TestActorBaseStoppedPreservesStopReason(t *testing.T) {
	reason := errors.New("handler failed")
	actorImpl := &terminationActor{}
	base := NewActorBase(context.Background(), actorImpl, "test")
	base.stopErr = reason
	if err := base.doReceive(&testActorContext{message: ActorStoppedMessage{}}); err != nil {
		t.Fatal(err)
	}
	if actorImpl.terminatedErr != reason {
		t.Fatalf("Terminate error = %v, want %v", actorImpl.terminatedErr, reason)
	}
}

type terminationActor struct{ terminatedErr error }

func (*terminationActor) Init(context.Context, []any) error        { return nil }
func (*terminationActor) DelayInit(context.Context) error          { return nil }
func (a *terminationActor) Terminate(_ context.Context, err error) { a.terminatedErr = err }
func (*terminationActor) Timer() *ActorTimer                       { return nil }
func (*terminationActor) Self() PID                                { return PID{} }
func (*terminationActor) HandleMessage(context.Context, any) error { return nil }

type testActorContext struct{ message any }

func (c *testActorContext) Sender() PID                      { return PID{} }
func (c *testActorContext) Message() any                     { return c.message }
func (c *testActorContext) MessageHeader() map[string]string { return nil }
func (c *testActorContext) Self() PID                        { return PID{} }
func (*testActorContext) Stop(PID)                           {}
func (*testActorContext) Watch(PID)                          {}
func (*testActorContext) Unwatch(PID)                        {}
func (*testActorContext) Children() []PID                    { return nil }

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
