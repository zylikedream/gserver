package gxyactor

import (
	"context"
	"errors"
	"testing"
	"time"

	"gserver/core/gxytimer"
)
type lifecycleProbeActor struct {
	*ActorBase
	events      []string
	initErr     error
	delayErr    error
	handlerErr  error
	panicHandle bool
}

func newLifecycleProbe() *lifecycleProbeActor {
	p := &lifecycleProbeActor{}
	p.ActorBase = NewActorBase(context.Background(), p, "lifecycle-test")
	return p
}

func (p *lifecycleProbeActor) Init(context.Context, []any) error {
	p.events = append(p.events, "init")
	return p.initErr
}
func (p *lifecycleProbeActor) DelayInit(context.Context) error {
	p.events = append(p.events, "delay-init")
	return p.delayErr
}
func (p *lifecycleProbeActor) HandleMessage(context.Context, any) error {
	p.events = append(p.events, "message")
	if p.panicHandle {
		panic("handler panic")
	}
	return p.handlerErr
}
func (p *lifecycleProbeActor) Terminate(context.Context, error) {
	p.events = append(p.events, "terminate")
}
func (p *lifecycleProbeActor) Timer() *ActorTimer { return p.ActorBase.timer }

type lifecycleContext struct {
	message any
	self    PID
	stops   []PID
}

func (c *lifecycleContext) Sender() PID                       { return PID{} }
func (c *lifecycleContext) Message() any                     { return c.message }
func (c *lifecycleContext) MessageHeader() map[string]string { return nil }
func (c *lifecycleContext) Self() PID                        { return c.self }
func (c *lifecycleContext) Stop(pid PID)                     { c.stops = append(c.stops, pid) }
func (*lifecycleContext) Watch(PID)                           {}
func (*lifecycleContext) Unwatch(PID)                         {}
func (*lifecycleContext) Children() []PID                     { return nil }

func TestLifecycleInitDelayInitMessageTerminateOrdering(t *testing.T) {
	probe := newLifecycleProbe()
	ctx := &lifecycleContext{self: PID{Runtime: "test", Node: "node", ID: "lifecycle", Creation: "1"}}
	probe.Receive(&lifecycleContext{message: ActorStartedMessage{Self: ctx.self}, self: ctx.self})
	if got, want := probe.events, []string{"init", "delay-init"}; !equalStrings(got, want) {
		t.Fatalf("startup events = %v, want %v", got, want)
	}
	ctx.message = "business"
	probe.Receive(ctx)
	if got, want := probe.events, []string{"init", "delay-init", "message"}; !equalStrings(got, want) {
		t.Fatalf("message events = %v, want %v", got, want)
	}
	ctx.message = ActorStoppedMessage{Err: errors.New("stop")}
	probe.Receive(ctx)
	probe.Receive(ctx)
	if got, want := probe.events, []string{"init", "delay-init", "message", "terminate"}; !equalStrings(got, want) {
		t.Fatalf("termination events = %v, want %v", got, want)
	}
}

func TestLifecycleInitFailureStopsWithoutBusinessMessage(t *testing.T) {
	probe := newLifecycleProbe()
	probe.initErr = errors.New("init failed")
	ctx := &lifecycleContext{self: PID{Runtime: "test", Node: "node", ID: "failed", Creation: "1"}}
	probe.Receive(&lifecycleContext{message: ActorStartedMessage{Self: ctx.self}, self: ctx.self})
	ctx.message = "business"
	probe.Receive(ctx)
	if containsString(probe.events, "message") {
		t.Fatalf("business message delivered after init failure: %v", probe.events)
	}
	if len(ctx.stops) == 0 {
		t.Fatal("init failure did not request actor stop")
	}
}

func TestActorHandlerPanicStopsCurrentActor(t *testing.T) {
	probe := newLifecycleProbe()
	probe.panicHandle = true
	ctx := &lifecycleContext{self: PID{Runtime: "test", Node: "node", ID: "panic", Creation: "1"}}
	probe.Receive(&lifecycleContext{message: ActorStartedMessage{Self: ctx.self}, self: ctx.self})
	ctx.message = "business"
	probe.Receive(ctx)
	if len(ctx.stops) == 0 {
		t.Fatal("handler panic did not request actor stop")
	}
}

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

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type lifecycleCronState struct {
	updated time.Time
}

func (s *lifecycleCronState) GetCronTm() time.Time { return time.Time{} }
func (s *lifecycleCronState) SetCronTm(tm time.Time) { s.updated = tm }

func TestTimerCronStateChangesOnlyWhenMailboxActivates(t *testing.T) {
	timer := NewActorTimer(PID{Runtime: "test", Node: "node", ID: "timer", Creation: "1"})
	state := &lifecycleCronState{}
	timer.SetCronState(state)
	timer.AddCron(context.Background(), gxytimer.NewCron("cron", "* * * * * *"), func(context.Context, gxytimer.TimerActiveInfo) {})
	if !state.updated.IsZero() {
		t.Fatal("timer source mutated cron state before mailbox delivery")
	}
	now := time.Now()
	if err := timer.Active(context.Background(), ActorTimerMsg{Name: "cron", Time: now}); err != nil {
		t.Fatal(err)
	}
	if !state.updated.Equal(now) {
		t.Fatalf("cron state time = %v, want %v", state.updated, now)
	}
	timer.Stop(context.Background())
}
