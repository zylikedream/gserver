package ergo

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"ergo.services/ergo"
	"ergo.services/ergo/gen"
	"google.golang.org/protobuf/proto"
	"gserver/core/gxyactor"
	"gserver/protocol/pb"
)

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
	if _, err := adapter.toErgoPID(gxyactor.PID{Runtime: "protoactor-v1", Node: "node@localhost", ID: "role/7", Creation: "42"}); !errors.Is(err, ErrUnknownPID) {
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
		actor := &adapterTestActor{ready: make(chan struct{})}
		actor.ActorBase = gxyactor.NewActorBase(context.Background(), actor, "test")
		return actor
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
	result, err := adapter.Call(context.Background(), pid, "ping", time.Second)
	if err != nil || result != "pong" {
		t.Fatalf("call result=%v err=%v", result, err)
	}
	start := time.Now()
	if _, err := adapter.Call(context.Background(), pid, "slow", 20*time.Millisecond); !errors.Is(err, ErrTimeout) {
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
	*gxyactor.ActorBase
}

func (a *initFailActor) Init(context.Context, []any) error {
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
		actor := &initFailActor{}
		actor.ActorBase = gxyactor.NewActorBase(context.Background(), actor, "test")
		return actor
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
func (*initFailActor) HandleMessage(context.Context, any) error { return nil }

type adapterTestActor struct {
	*gxyactor.ActorBase
	ready chan struct{}
}

func (a *adapterTestActor) HandleMessage(_ context.Context, message any) error {
	if message == "send" {
		close(a.ready)
	}
	return nil
}
func (a *adapterTestActor) DoCallMsgHandler(_ context.Context, message any) (any, error) {
	switch message {
	case "ping":
		return "pong", nil
	case "slow":
		time.Sleep(time.Second)
		return "late", nil
	case "error":
		return nil, errors.New("handler error")
	default:
		return nil, errors.New("unknown request")
	}
}
func TestAdapterResponseErrorAndUnknownPID(t *testing.T) {
	node, err := ergo.StartNode("adapter-error@localhost", gen.NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer node.StopWithTimeout(time.Second)
	adapter := New(node, "adapter-error@localhost")
	if err := adapter.RegisterActorKind("test", func() gxyactor.IActor {
		actor := &adapterTestActor{}
		actor.ActorBase = gxyactor.NewActorBase(context.Background(), actor, "test")
		return actor
	}); err != nil {
		t.Fatal(err)
	}
	pid, err := adapter.Spawn("test", "test/2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Call(context.Background(), pid, "error", time.Second); err == nil {
		t.Fatal("handler response error became a successful value")
	}
	if _, err := adapter.Call(context.Background(), pid, "ping", time.Second); !errors.Is(err, ErrUnknownPID) && !errors.Is(err, ErrActorStopped) {
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
		actor := &adapterTestActor{}
		actor.ActorBase = gxyactor.NewActorBase(context.Background(), actor, "test")
		return actor
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
