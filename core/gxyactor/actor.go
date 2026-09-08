package gxyactor

import "gserver/core/gxytimer"

type ActorTimerMsg gxytimer.TimerActiveInfo

type ActorProducer func() IActor

// IActor is the runtime-neutral business behavior driven by an ActorProcess.
// Every callback runs serially on the owning runtime process.
type IActor interface {
	Init(ActorContext, []any) error
	DelayInit(ActorContext) error
	HandleMessage(ActorContext, any) error
	Terminate(ActorContext, error)
}

type IUnspanMessage interface{ Unspan() }
type unspanMessage struct{}

func (*unspanMessage) Unspan() {}
