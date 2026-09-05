package ergo

import (
	"context"
	"sync"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"gserver/core/gxyactor"
)

type ergoActor struct {
	act.Actor
	adapter  *Adapter
	kind     string
	callerID string
	id       string
	actor    gxyactor.IActor
	ctx      *actorContext
	once     sync.Once
}

func newErgoActor(adapter *Adapter, kind, callerID, id string, producer gxyactor.ActorProducer) *ergoActor {
	return &ergoActor{adapter: adapter, kind: kind, callerID: callerID, id: id, actor: producer()}
}

func (a *ergoActor) Init(args ...any) error {
	if a.actor == nil {
		return ErrActorInitFailed
	}
	a.adapter.remember(a.kind, a.callerID, a.PID())
	a.ctx = &actorContext{adapter: a.adapter, process: a, message: gxyactor.ActorStartedMessage{Self: a.adapter.fromErgoPID(a.PID(), a.id), InitArgs: args}}
	if receiver, ok := a.actor.(interface{ Receive(gxyactor.ActorContext) }); ok {
		receiver.Receive(a.ctx)
		return nil
	}
	if err := a.actor.Init(context.Background(), args); err != nil {
		return wrap(ErrActorInitFailed, err)
	}
	return nil
}

func (a *ergoActor) HandleMessage(from gen.PID, message any) error {
	decoded, err := a.adapter.decode(message)
	if err != nil {
		return err
	}
	if a.ctx == nil {
		a.ctx = &actorContext{adapter: a.adapter, process: a}
	}
	a.ctx.sender = a.adapter.fromErgoPID(from, "")
	a.ctx.message = decoded
	if receiver, ok := a.actor.(interface{ Receive(gxyactor.ActorContext) }); ok {
		receiver.Receive(a.ctx)
		return nil
	}
	return a.actor.HandleMessage(context.Background(), decoded)
}

func (a *ergoActor) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	decoded, err := a.adapter.decode(request)
	if err != nil {
		return nil, err
	}
	if a.ctx == nil {
		a.ctx = &actorContext{adapter: a.adapter, process: a}
	}
	a.ctx.sender = a.adapter.fromErgoPID(from, "")
	a.ctx.request = &ergoRequest{sender: a.ctx.sender, rawSender: from, ref: ref, process: a}
	a.ctx.message = decoded
	handler, ok := a.actor.(interface {
		DoCallMsgHandler(context.Context, any) (any, error)
	})
	if !ok {
		if sendErr := a.SendResponseError(from, ref, gen.ErrUnsupported); sendErr != nil {
			return nil, sendErr
		}
		return nil, nil
	}
	result, err := handler.DoCallMsgHandler(context.Background(), decoded)
	if err != nil {
		if sendErr := a.SendResponseError(from, ref, err); sendErr != nil {
			return nil, sendErr
		}
		return nil, nil
	}
	if result == nil {
		return nil, nil
	}
	wire, err := a.adapter.encodeFor(a.ctx.sender, result)
	if err != nil {
		return nil, err
	}
	return wire, nil
}

func (a *ergoActor) Terminate(reason error) {
	a.once.Do(func() {
		if a.actor == nil {
			return
		}
		if receiver, ok := a.actor.(interface{ Receive(gxyactor.ActorContext) }); ok {
			if a.ctx == nil {
				a.ctx = &actorContext{adapter: a.adapter, process: a}
			}
			a.ctx.message = gxyactor.ActorStoppedMessage{Err: reason}
			receiver.Receive(a.ctx)
		} else {
			a.actor.Terminate(context.Background(), reason)
		}
		if a.adapter != nil {
			a.adapter.Forget(a.adapter.fromErgoPID(a.PID(), a.id))
		}
	})
}

type ergoRequest struct {
	sender    gxyactor.PID
	rawSender gen.PID
	ref       gen.Ref
	process   *ergoActor
}

func (r *ergoRequest) Sender() gxyactor.PID {
	if r == nil {
		return gxyactor.PID{}
	}
	return r.sender
}

type actorContext struct {
	adapter *Adapter
	process *ergoActor
	sender  gxyactor.PID
	message any
	request *ergoRequest
}

func (c *actorContext) Sender() gxyactor.PID {
	if c == nil {
		return gxyactor.PID{}
	}
	return c.sender
}
func (c *actorContext) Message() any {
	if c == nil {
		return nil
	}
	return c.message
}
func (c *actorContext) MessageHeader() map[string]string { return nil }
func (c *actorContext) Self() gxyactor.PID {
	if c == nil || c.adapter == nil || c.process == nil {
		return gxyactor.PID{}
	}
	return c.adapter.fromErgoPID(c.process.PID(), c.process.id)
}
func (c *actorContext) Stop(pid gxyactor.PID) {
	if c == nil || c.adapter == nil {
		return
	}
	_ = c.adapter.Stop(pid)
}
func (c *actorContext) Watch(pid gxyactor.PID) {
	if c == nil || c.process == nil {
		return
	}
	if raw, err := c.adapter.toErgoPID(pid); err == nil {
		_ = c.process.MonitorPID(raw)
	}
}
func (c *actorContext) Unwatch(pid gxyactor.PID) {
	if c == nil || c.process == nil {
		return
	}
	if raw, err := c.adapter.toErgoPID(pid); err == nil {
		_ = c.process.DemonitorPID(raw)
	}
}
func (c *actorContext) Children() []gxyactor.PID { return nil }
