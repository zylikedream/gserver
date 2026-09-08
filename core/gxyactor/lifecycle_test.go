package gxyactor

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
	"gserver/core/gxytimer"
	"gserver/core/gxyutil"
	"gserver/protocol/pb"
)

type lifecycleProbeActor struct {
	events       []string
	terminateErr error
	initErr      error
	delayErr     error
	handlerErr   error
	panicHandle  bool
}

func (p *lifecycleProbeActor) Init(ActorContext, []any) error {
	p.events = append(p.events, "init")
	return p.initErr
}
func (p *lifecycleProbeActor) DelayInit(ActorContext) error {
	p.events = append(p.events, "delay-init")
	return p.delayErr
}
func (p *lifecycleProbeActor) HandleMessage(ActorContext, any) error {
	p.events = append(p.events, "message")
	if p.panicHandle {
		panic("handler panic")
	}
	return p.handlerErr
}
func (p *lifecycleProbeActor) Terminate(_ ActorContext, err error) {
	p.events = append(p.events, "terminate")
	p.terminateErr = err
}

func TestActorProcessMapsLifecycleDirectly(t *testing.T) {
	probe := &lifecycleProbeActor{}
	process := NewActorProcess("lifecycle-test", probe)
	ctx := newLifecycleContext(process)

	if err := process.Init(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := process.HandleMessage(ctx, "business"); err != nil {
		t.Fatal(err)
	}
	process.Terminate(ctx, errors.New("stop"))

	if got, want := probe.events, []string{"init", "delay-init", "message", "terminate"}; !equalStrings(got, want) {
		t.Fatalf("lifecycle events = %v, want %v", got, want)
	}
}

func TestActorProcessHandlerErrorTerminatesWithOriginalReason(t *testing.T) {
	reason := errors.New("handler failed")
	probe := &lifecycleProbeActor{handlerErr: reason}
	process := NewActorProcess("lifecycle-test", probe)
	ctx := newLifecycleContext(process)
	if err := process.Init(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := process.HandleMessage(ctx, "business"); !errors.Is(err, reason) {
		t.Fatalf("handle error = %v, want %v", err, reason)
	}
	process.SetStopReason(reason)
	process.Terminate(ctx, nil)
	if !errors.Is(probe.terminateErr, reason) {
		t.Fatalf("terminate reason = %v, want %v", probe.terminateErr, reason)
	}
}

func TestActorProcessRegistersAndCallsHandlers(t *testing.T) {
	probe := &lifecycleProbeActor{}
	process := NewActorProcess("lifecycle-test", probe)
	ctx := newLifecycleContext(process)
	if err := process.Init(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := process.AutoHandleMsg(ctx, &pb.ReqGuildInfo{}); err == nil {
		t.Fatal("expected unknown handler error")
	}
	process.AddMsgHandler(lifecycleValueHandler{})
	result, err := process.HandleCall(ctx, &pb.ReqGuildInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("handler returned nil result")
	}
}

type lifecycleValueHandler struct{}

func (lifecycleValueHandler) HandleValue(context.Context, *pb.ReqGuildInfo) (*pb.RspGuildInfo, error) {
	return &pb.RspGuildInfo{}, nil
}

type lifecycleContext struct {
	context.Context
	process *ActorProcess
	self    PID
	stops   []error
}

func newLifecycleContext(process *ActorProcess) *lifecycleContext {
	return &lifecycleContext{Context: context.Background(), process: process, self: PID{Runtime: "test", Node: "node", ID: "actor", Creation: "1"}}
}
func (c *lifecycleContext) Self() PID { return c.self }
func (*lifecycleContext) Sender() PID { return PID{} }
func (c *lifecycleContext) Stop(err error) {
	c.stops = append(c.stops, err)
	c.process.SetStopReason(err)
}
func (*lifecycleContext) Watch(PID)                                          {}
func (*lifecycleContext) Unwatch(PID)                                        {}
func (*lifecycleContext) Children() []PID                                    { return nil }
func (c *lifecycleContext) Timer() *ActorTimer                               { return c.process.Timer() }
func (c *lifecycleContext) Span() trace.Span                                 { return trace.SpanFromContext(c) }
func (*lifecycleContext) SetLogValue(string, any)                            {}
func (*lifecycleContext) AddMsgHandler(any, ...string) []*gxyutil.MethodMeta { return nil }
func (*lifecycleContext) AutoHandleMsg(any) (any, error)                     { return nil, nil }
func (*lifecycleContext) Respond(any, ...error) error                        { return nil }

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type lifecycleCronState struct{ updated time.Time }

func (s *lifecycleCronState) GetCronTm() time.Time   { return time.Time{} }
func (s *lifecycleCronState) SetCronTm(tm time.Time) { s.updated = tm }

func TestTimerCronStateChangesOnlyWhenMailboxActivates(t *testing.T) {
	process := NewActorProcess("timer-test", &lifecycleProbeActor{})
	ctx := newLifecycleContext(process)
	if err := process.Init(ctx, nil); err != nil {
		t.Fatal(err)
	}
	state := &lifecycleCronState{}
	timer := process.Timer()
	timer.SetCronState(state)
	timer.AddCron(context.Background(), gxytimer.NewCron("cron", "* * * * * *"), func(ActorContext, gxytimer.TimerActiveInfo) {})
	if !state.updated.IsZero() {
		t.Fatal("timer source mutated cron state before mailbox delivery")
	}
	now := time.Now()
	if err := timer.Active(ctx, ActorTimerMsg{Name: "cron", Time: now}); err != nil {
		t.Fatal(err)
	}
	if !state.updated.Equal(now) {
		t.Fatalf("cron state time = %v, want %v", state.updated, now)
	}
	timer.Stop(context.Background())
}
