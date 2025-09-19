package logic

import (
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/message"
)

type GateHandler struct {
	endpoint.BaseEventHandler
}

func NewGateHandler() *GateHandler {
	return &GateHandler{}
}

func (es *GateHandler) OnMessage(ep endpoint.Endpoint, msg *message.Message) error {
	return nil
}

func (es *GateHandler) OnClose(ep endpoint.Endpoint) {
}
