package logic

import (
	"context"

	"gserver/core/gxyactor"
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/message"

	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/os/glog"
)

type GateHandler struct {
	endpoint.BaseEventHandler
}

func NewGateHandler() *GateHandler {
	return &GateHandler{}
}

func (gh *GateHandler) OnOpen(ep endpoint.Endpoint) error {
	connID := ep.Conn().RemoteAddr().String()
	glog.Debugf(context.Background(), "New connection: %s", connID)

	// 通过SessionManager创建Session Actor
	sessionMgr := SessionManager()
	err := sessionMgr.CreateSession(ep)
	if err != nil {
		glog.Errorf(context.Background(), "Failed to create session for %s: %v", connID, err)
		ep.Conn().Close()
	}
	return nil
}

func (gh *GateHandler) OnMessage(ep endpoint.Endpoint, msg *message.Message) error {
	sessPid, ok := ep.GetData().(gen.PID)
	if !ok {
		glog.Errorf(context.Background(), "Failed to get session from endpoint data")
		return nil
	}
	gxyactor.ActorSystem().Send(sessPid, gxyactor.NewActorMessage(gxyactor.MsgClient, *msg))
	// 消息将直接由Session Actor处理
	return nil
}

func (gh *GateHandler) OnClose(ep endpoint.Endpoint, err error) {
	sessionMgr := SessionManager()
	sessionMgr.StopSession(ep, err)
}
