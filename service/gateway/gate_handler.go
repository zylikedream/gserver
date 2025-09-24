package gateway

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
	sessPid, err := GateService().SpawnSession(ep)
	if err != nil {
		glog.Errorf(context.Background(), "Failed to create session for %s: %+v", connID, err)
		ep.Conn().Close()
		return err
	}
	ep.SetData(sessPid)
	glog.Debugf(context.Background(), "Session Actor created with PID: %v", sessPid)
	return nil
}

func (gh *GateHandler) OnMessage(ep endpoint.Endpoint, msg *message.Message) error {
	sessPid, ok := ep.GetData().(gen.PID)
	if !ok {
		glog.Errorf(context.Background(), "Failed to get session from endpoint data")
		return nil
	}
	gxyactor.ActorSystem().Send(sessPid, gxyactor.NewActorMessage(gxyactor.MsgClientReq, *msg))
	// 消息将直接由Session Actor处理
	return nil
}

func (gh *GateHandler) OnClose(ep endpoint.Endpoint, err error) {
	sessPid, ok := ep.GetData().(gen.PID)
	if ok {
		GateService().StopSession(sessPid)
	}
}
