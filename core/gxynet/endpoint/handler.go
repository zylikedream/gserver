package endpoint

import "gserver/core/gxynet/message"

type EventHandler interface {
	OnOpen(Endpoint) error
	OnClose(Endpoint, error)
	OnError(Endpoint, error)
	OnMessage(Endpoint, *message.Message) error
}

type BaseEventHandler struct {
}

func (e *BaseEventHandler) OnOpen(Endpoint) error {
	return nil
}

func (e *BaseEventHandler) OnClose(Endpoint, error) {
}

func (e *BaseEventHandler) OnError(Endpoint, error) {
}

func (e *BaseEventHandler) OnMessage(Endpoint, *message.Message) error {
	return nil
}
