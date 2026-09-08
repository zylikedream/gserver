package logic

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"gserver/core/gxyactor"
	"gserver/core/gxyutil"
)

type roleTestContext struct {
	context.Context
	self      gxyactor.PID
	sender    gxyactor.PID
	timer     *gxyactor.ActorTimer
	stopped   error
	responses []any
}

var roleTestHandler = gxyutil.NewMsgHandler()

func newRoleTestContext() *roleTestContext {
	self := gxyactor.PID{Runtime: "test", Node: "node", ID: "role", Creation: "1"}
	return &roleTestContext{
		Context: context.Background(),
		self:    self,
		sender:  gxyactor.PID{Runtime: "test", Node: "node", ID: "session", Creation: "1"},
		timer:   gxyactor.NewActorTimer(self),
	}
}

func (c *roleTestContext) Self() gxyactor.PID          { return c.self }
func (c *roleTestContext) Sender() gxyactor.PID        { return c.sender }
func (c *roleTestContext) Stop(err error)              { c.stopped = err }
func (*roleTestContext) Watch(gxyactor.PID)            {}
func (*roleTestContext) Unwatch(gxyactor.PID)          {}
func (*roleTestContext) Children() []gxyactor.PID      { return nil }
func (c *roleTestContext) Timer() *gxyactor.ActorTimer { return c.timer }
func (c *roleTestContext) Span() trace.Span            { return trace.SpanFromContext(c) }
func (*roleTestContext) SetLogValue(string, any)       {}
func (*roleTestContext) AddMsgHandler(handler any, prefix ...string) []*gxyutil.MethodMeta {
	return roleTestHandler.AddHandler(handler, prefix...)
}
func (*roleTestContext) AutoHandleMsg(message any) (any, error) {
	return roleTestHandler.CallWithMsg(context.Background(), message)
}
func (c *roleTestContext) Respond(message any, responseErr ...error) error {
	if message != nil {
		c.responses = append(c.responses, message)
	}
	if len(responseErr) > 0 && responseErr[0] != nil {
		return responseErr[0]
	}
	return nil
}
