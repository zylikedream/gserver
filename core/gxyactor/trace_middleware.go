package gxyactor

import "github.com/asynkron/protoactor-go/actor"

// tracePropagationMiddleware handles trace header promotion through nested envelopes.
//
// RequestFuture and RequestWithCustomSender wrap the message in an additional
// envelope, burying trace headers inside. This middleware promotes headers from
// the inner envelope to the outer one and unwraps the message.
func tracePropagationMiddleware() actor.SenderMiddleware {
	return func(next actor.SenderFunc) actor.SenderFunc {
		return func(c actor.SenderContext, target *actor.PID, envelope *actor.MessageEnvelope) {
			if inner, ok := envelope.Message.(*actor.MessageEnvelope); ok && len(inner.Header) > 0 {
				for _, k := range inner.Header.Keys() {
					envelope.SetHeader(k, inner.Header.Get(k))
				}
				envelope.Message = inner.Message
			}
			next(c, target, envelope)
		}
	}
}
