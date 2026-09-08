package ergo

import (
	"context"
	"encoding/binary"
	"ergo.services/ergo"
	"ergo.services/ergo/gen"
	"errors"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"
	"gserver/core/gxyactor"
	"gserver/protocol/pb"
	"strconv"
	"testing"
	"time"
)

type DirectLifecycleCall struct{}

type directLifecycleActor struct {
	initArgs   chan []any
	delayInit  chan struct{}
	message    chan any
	terminated chan error
}

func (a *directLifecycleActor) Init(_ gxyactor.ActorContext, args []any) error {
	a.initArgs <- args
	return nil
}
func (a *directLifecycleActor) DelayInit(gxyactor.ActorContext) error {
	close(a.delayInit)
	return nil
}
func (a *directLifecycleActor) HandleMessage(_ gxyactor.ActorContext, message any) error {
	a.message <- message
	return nil
}
func (a *directLifecycleActor) Terminate(_ gxyactor.ActorContext, reason error) {
	a.terminated <- reason
}
func (*directLifecycleActor) HandleDirectLifecycleCall(context.Context, *DirectLifecycleCall) (string, error) {
	return "called", nil
}

func TestAdapterDirectActorLifecycle(t *testing.T) {
	node, err := ergo.StartNode("direct-lifecycle@localhost", gen.NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer node.StopWithTimeout(time.Second)

	adapter := New(node, "direct-lifecycle@localhost")
	probe := &directLifecycleActor{
		initArgs:   make(chan []any, 1),
		delayInit:  make(chan struct{}),
		message:    make(chan any, 1),
		terminated: make(chan error, 1),
	}
	if err := adapter.RegisterActorKind("direct", func() gxyactor.IActor { return probe }); err != nil {
		t.Fatal(err)
	}
	pid, err := adapter.Spawn("direct", "1", "arg")
	if err != nil {
		t.Fatal(err)
	}
	if args := <-probe.initArgs; len(args) != 1 || args[0] != "arg" {
		t.Fatalf("init args = %#v, want [arg]", args)
	}
	select {
	case <-probe.delayInit:
	default:
		t.Fatal("DelayInit was not called before Spawn returned")
	}
	if err := adapter.Send(context.Background(), pid, "message"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-probe.message:
		if got != "message" {
			t.Fatalf("message = %#v, want message", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
	result, err := adapter.Call(context.Background(), pid, &DirectLifecycleCall{}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result != "called" {
		t.Fatalf("call result = %#v, want called", result)
	}
	if err := adapter.Stop(pid); err != nil {
		t.Fatal(err)
	}
	select {
	case <-probe.terminated:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for termination")
	}
}

func TestTraceHopContextPreservesErgoIdentity(t *testing.T) {
	node, err := ergo.StartNode("trace-hop@localhost", gen.NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer node.StopWithTimeout(time.Second)
	adapter := New(node, "trace-hop@localhost")
	gxyactor.SetRuntime(adapter)
	t.Cleanup(func() { gxyactor.SetRuntime(nil) })

	tracing := gen.Tracing{ID: [2]uint64{11, 22}, SpanID: 33}
	received := make(chan trace.SpanContext, 1)
	target := &traceHopTarget{received: received}
	if err := adapter.RegisterActorKind("trace-target", func() gxyactor.IActor {
		return target
	}); err != nil {
		t.Fatal(err)
	}
	source := &traceHopSource{adapter: adapter, tracing: tracing}
	if err := adapter.RegisterActorKind("trace-source", func() gxyactor.IActor {
		return source
	}); err != nil {
		t.Fatal(err)
	}
	targetPID, err := adapter.Spawn("trace-target", "target")
	if err != nil {
		t.Fatal(err)
	}
	source.target = targetPID
	sourcePID, err := adapter.Spawn("trace-source", "source")
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Send(context.Background(), sourcePID, "hop"); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-received:
		if got.TraceID() != traceID(tracing) {
			t.Fatalf("trace = %s, want %s", got.TraceID(), traceID(tracing))
		}
		if got.SpanID() == (trace.SpanID{}) {
			t.Fatal("receiving callback context has no transport span ID")
		}
		if !got.IsRemote() {
			t.Fatal("receiving callback context must be marked remote")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for traced actor hop")
	}
}

type traceHopSource struct {
	adapter *Adapter
	target  gxyactor.PID
	tracing gen.Tracing
}

func (a *traceHopSource) HandleMessage(ctx gxyactor.ActorContext, message any) error {
	if message != "hop" {
		return nil
	}
	process, ok := ctx.Value(processContextKey{}).(gen.Process)
	if !ok {
		return errors.New("source callback did not retain Ergo process")
	}
	process.SetPropagatingTrace(a.tracing)
	return a.adapter.Send(ctx, a.target, "hop")
}
func (*traceHopSource) Init(gxyactor.ActorContext, []any) error { return nil }
func (*traceHopSource) DelayInit(gxyactor.ActorContext) error   { return nil }
func (*traceHopSource) Terminate(gxyactor.ActorContext, error)  {}

type traceHopTarget struct {
	received chan trace.SpanContext
}

func (a *traceHopTarget) HandleMessage(ctx gxyactor.ActorContext, _ any) error {
	a.received <- trace.SpanContextFromContext(ctx)
	return nil
}
func (*traceHopTarget) Init(gxyactor.ActorContext, []any) error { return nil }
func (*traceHopTarget) DelayInit(gxyactor.ActorContext) error   { return nil }
func (*traceHopTarget) Terminate(gxyactor.ActorContext, error)  {}

func traceID(tracing gen.Tracing) trace.TraceID {
	var id trace.TraceID
	binary.BigEndian.PutUint64(id[:8], tracing.ID[0])
	binary.BigEndian.PutUint64(id[8:], tracing.ID[1])
	return id
}

func TestErgoCallbackContextStartsFreshPerMessage(t *testing.T) {
	type valueKey struct{}
	cancelled := context.WithValue(context.Background(), valueKey{}, "prior")
	cancelled, cancel := context.WithCancel(cancelled)
	cancel()
	fresh := contextWithErgoTrace(context.Background(), gen.Tracing{}, nil)
	if fresh.Value(valueKey{}) != nil {
		t.Fatal("fresh callback context retained a prior value chain")
	}
	if fresh.Err() != nil {
		t.Fatalf("fresh callback context is cancelled: %v", fresh.Err())
	}
	if cancelled.Value(valueKey{}) != "prior" || cancelled.Err() == nil {
		t.Fatal("test setup did not create a cancelled prior context")
	}
}

func TestEnvelopeRoundTripRegisteredProto(t *testing.T) {
	registry := NewMessageRegistry()
	if err := registry.Register("login.request", func() proto.Message { return &pb.ReqAccountLogin{} }); err != nil {
		t.Fatal(err)
	}
	original := &pb.ReqAccountLogin{}
	envelope, err := registry.Encode(original)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := registry.Decode(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded.(*pb.ReqAccountLogin); !ok {
		t.Fatalf("decoded type %T", decoded)
	}
}

func TestEnvelopeUnknownAndMalformedAreTyped(t *testing.T) {
	registry := NewMessageRegistry()
	if _, err := registry.Decode(GServerEnvelope{Type: "missing", Data: []byte{1}}); !errors.Is(err, ErrUnknownMessageType) {
		t.Fatalf("unknown type error = %v", err)
	}
	if err := registry.Register("login.request", func() proto.Message { return &pb.ReqAccountLogin{} }); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Decode(GServerEnvelope{Type: "login.request", Data: []byte{0xff}}); !errors.Is(err, ErrMalformedPayload) {
		t.Fatalf("malformed error = %v", err)
	}
}

func TestPIDConversionPreservesCreationAndRejectsWrongRuntime(t *testing.T) {
	adapter := &Adapter{nodeName: "node@localhost", creation: 42}
	raw := gen.PID{Node: "node@localhost", ID: 7, Creation: 42}
	normalized := adapter.fromErgoPID(raw, "role/7")
	if normalized != (gxyactor.PID{Runtime: RuntimeID, Node: "node@localhost", ID: "role/7", Creation: "42"}) {
		t.Fatalf("normalized PID = %+v", normalized)
	}
	if _, err := adapter.toErgoPID(gxyactor.PID{Runtime: "other-runtime", Node: "node@localhost", ID: "role/7", Creation: "42"}); !errors.Is(err, ErrUnknownPID) {
		t.Fatalf("wrong runtime error = %v", err)
	}
	if _, err := adapter.toErgoPID(gxyactor.PID{Runtime: RuntimeID, Node: "node@localhost", ID: "role/7", Creation: "41"}); !errors.Is(err, ErrUnknownPID) {
		t.Fatalf("stale creation error = %v", err)
	}
}
func TestAdapterLocalSendCallTimeoutAndStop(t *testing.T) {
	node, err := ergo.StartNode("adapter-test@localhost", gen.NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer node.StopWithTimeout(time.Second)
	adapter := New(node, "adapter-test@localhost")
	gxyactor.SetRuntime(adapter)
	if err := adapter.RegisterActorKind("test", func() gxyactor.IActor {
		return &adapterTestActor{ready: make(chan struct{})}
	}); err != nil {
		t.Fatal(err)
	}
	pid, err := adapter.Spawn("test", "1")
	if err != nil {
		t.Fatal(err)
	}
	if actors := adapter.GetLocalActorAll("test"); len(actors) != 1 {
		t.Fatalf("local actor entries = %d, want 1 (%+v)", len(actors), actors)
	}
	if err := adapter.Send(context.Background(), pid, "send"); err != nil {
		t.Fatal(err)
	}
	if pid.ID != "test/1" {
		t.Fatalf("normalized logical ID = %q, want test/1", pid.ID)
	}
	result, err := adapter.Call(context.Background(), pid, &AdapterPing{}, time.Second)
	if err != nil || result != "pong" {
		t.Fatalf("call result=%v err=%v", result, err)
	}
	start := time.Now()
	if _, err := adapter.Call(context.Background(), pid, &AdapterSlow{}, 20*time.Millisecond); !errors.Is(err, ErrTimeout) {
		t.Fatalf("slow call error=%v", err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("call ignored caller timeout: %s", time.Since(start))
	}
	if err := adapter.Stop(pid); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Stop(pid); err != nil {
		t.Fatal(err)
	}
}

type initFailActor struct {
}

func (a *initFailActor) Init(gxyactor.ActorContext, []any) error {
	return errors.New("init failed")
}

func TestAdapterInitFailureDoesNotPublishPID(t *testing.T) {
	node, err := ergo.StartNode("adapter-init-fail@localhost", gen.NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer node.StopWithTimeout(time.Second)
	adapter := New(node, "adapter-init-fail@localhost")
	if err := adapter.RegisterActorKind("test", func() gxyactor.IActor {
		return &initFailActor{}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Spawn("test", "failed"); !errors.Is(err, ErrActorInitFailed) {
		t.Fatalf("spawn error = %v, want ErrActorInitFailed", err)
	}
	if actors := adapter.GetLocalActorAll("test"); len(actors) != 0 {
		t.Fatalf("failed actor was published: %v", actors)
	}
}
func (*initFailActor) DelayInit(gxyactor.ActorContext) error          { return nil }
func (*initFailActor) HandleMessage(gxyactor.ActorContext, any) error { return nil }
func (*initFailActor) Terminate(gxyactor.ActorContext, error)         {}

type adapterTestActor struct {
	ready chan struct{}
}

func (*adapterTestActor) Init(gxyactor.ActorContext, []any) error { return nil }
func (*adapterTestActor) DelayInit(gxyactor.ActorContext) error   { return nil }
func (a *adapterTestActor) HandleMessage(_ gxyactor.ActorContext, message any) error {
	if message == "send" && a.ready != nil {
		close(a.ready)
	}
	return nil
}
func (*adapterTestActor) Terminate(gxyactor.ActorContext, error) {}

type AdapterPing struct{}
type AdapterSlow struct{}
type AdapterError struct{}

func (*adapterTestActor) HandleAdapterPing(context.Context, *AdapterPing) (string, error) {
	return "pong", nil
}
func (*adapterTestActor) HandleAdapterSlow(context.Context, *AdapterSlow) (string, error) {
	time.Sleep(time.Second)
	return "late", nil
}
func (*adapterTestActor) HandleAdapterError(context.Context, *AdapterError) (string, error) {
	return "", errors.New("handler error")
}
func TestAdapterResponseErrorAndUnknownPID(t *testing.T) {
	node, err := ergo.StartNode("adapter-error@localhost", gen.NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer node.StopWithTimeout(time.Second)
	adapter := New(node, "adapter-error@localhost")
	if err := adapter.RegisterActorKind("test", func() gxyactor.IActor {
		return &adapterTestActor{}
	}); err != nil {
		t.Fatal(err)
	}
	pid, err := adapter.Spawn("test", "test/2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Call(context.Background(), pid, &AdapterError{}, time.Second); err == nil {
		t.Fatal("handler response error became a successful value")
	}
	if _, err := adapter.Call(context.Background(), pid, &AdapterPing{}, time.Second); !errors.Is(err, ErrUnknownPID) && !errors.Is(err, ErrActorStopped) {
		t.Fatalf("actor remained callable after handler error: err=%v", err)
	}
}
func TestMapErrorNoRouteIsRemoteUnavailable(t *testing.T) {
	if err := mapError(gen.ErrNoRoute); !errors.Is(err, ErrRemoteNodeUnavailable) {
		t.Fatalf("no-route error = %v", err)
	}
}

func TestPIDMapMissFailsClosedAndResolverIsExplicit(t *testing.T) {
	pid := gxyactor.PID{Runtime: RuntimeID, Node: "node@localhost", ID: "role/7", Creation: "42"}
	adapter := &Adapter{nodeName: "node@localhost", creation: 42}
	if _, err := adapter.toErgoPID(pid); !errors.Is(err, ErrUnknownPID) {
		t.Fatalf("map miss error = %v", err)
	}
	adapter.resolvePID = func(gxyactor.PID) (gen.PID, error) {
		return gen.PID{Node: "node@localhost", ID: 7, Creation: 42}, nil
	}
	raw, err := adapter.toErgoPID(pid)
	if err != nil || raw.ID != 7 {
		t.Fatalf("resolver result=%+v err=%v", raw, err)
	}
}

func TestMessageRegistryDuplicateRegistrationIsTypedAndIdempotent(t *testing.T) {
	registry := NewMessageRegistry()
	constructor := func() proto.Message { return &pb.ReqAccountLogin{} }
	if err := registry.Register("login.request", constructor); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("login.request", constructor); err != nil {
		t.Fatalf("same registration should be idempotent: %v", err)
	}
	if err := registry.Register("login.other", constructor); !errors.Is(err, ErrDuplicateRegistration) {
		t.Fatalf("duplicate type error = %v", err)
	}
	if err := registry.Register("login.request", func() proto.Message { return &pb.RspAccountLogin{} }); !errors.Is(err, ErrDuplicateRegistration) {
		t.Fatalf("duplicate name error = %v", err)
	}
}
func TestStartWiresActivationAndPIDResolver(t *testing.T) {
	activationCalled := false
	resolverCalled := false
	adapter, err := Start(Options{
		NodeName: "adapter-both@localhost",
		Activation: func(context.Context, string, string, bool) (gxyactor.PID, error) {
			activationCalled = true
			return gxyactor.PID{Runtime: RuntimeID, Node: "adapter-both@localhost", ID: "test/1", Creation: "1"}, nil
		},
		ResolvePID: func(pid gxyactor.PID) (gen.PID, error) {
			resolverCalled = true
			creation, err := strconv.ParseInt(pid.Creation, 10, 64)
			if err != nil {
				return gen.PID{}, err
			}
			return gen.PID{Node: "adapter-both@localhost", ID: 1, Creation: creation}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.StopNode(time.Second)
	if _, err := adapter.ActivateActor(context.Background(), "test", "1", true); err != nil {
		t.Fatal(err)
	}
	if !activationCalled {
		t.Fatal("Start did not wire Options.Activation")
	}
	pid := gxyactor.PID{Runtime: RuntimeID, Node: "remote-node@localhost", ID: "test/1", Creation: strconv.FormatInt(adapter.Node().Creation(), 10)}
	if _, err := adapter.PIDToErgo(pid); err != nil {
		t.Fatal(err)
	}
	if !resolverCalled {
		t.Fatal("Start did not wire Options.ResolvePID")
	}
}
func TestSpawnCallerIDFormsUseOneMapEntryAndForget(t *testing.T) {
	node, err := ergo.StartNode("adapter-ids@localhost", gen.NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer node.StopWithTimeout(time.Second)
	adapter := New(node, "adapter-ids@localhost")
	gxyactor.SetRuntime(adapter)
	if err := adapter.RegisterActorKind("test", func() gxyactor.IActor {
		return &adapterTestActor{}
	}); err != nil {
		t.Fatal(err)
	}
	for _, callerID := range []string{"2", "test/2"} {
		pid, err := adapter.Spawn("test", callerID)
		if err != nil {
			t.Fatal(err)
		}
		if pid.ID != "test/2" {
			t.Fatalf("normalized PID ID = %q, want test/2", pid.ID)
		}
		if got := len(adapter.GetLocalActorAll("test")); got != 1 {
			t.Fatalf("local actor entries for %q = %d, want 1", callerID, got)
		}
		if err := adapter.Stop(pid); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		for len(adapter.GetLocalActorAll("test")) != 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if got := len(adapter.GetLocalActorAll("test")); got != 0 {
			t.Fatalf("local actor entries after stopping %q = %d, want 0", callerID, got)
		}
	}
}
