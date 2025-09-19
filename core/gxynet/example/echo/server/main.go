package main

import (
	"context"

	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/example/echo/proto"
	"gserver/core/gxynet/message"

	"github.com/gogf/gf/v2/os/glog"
	protobuf "google.golang.org/protobuf/proto"
)

var ctx = context.Background()

func main() {
	EchoServer()
}

type EchoEventHandler struct {
	endpoint.BaseEventHandler
}

func (e *EchoEventHandler) OnOpen(ep endpoint.Endpoint) error {
	glog.Infof(ctx, "conn open, addr=%s", ep.Conn().RemoteAddr())
	return nil
}

func (e *EchoEventHandler) OnClose(ep endpoint.Endpoint) {
	glog.Infof(ctx, "conn close, addr=%s", ep.Conn().RemoteAddr())
}

func (e *EchoEventHandler) OnMessage(ep endpoint.Endpoint, msg *message.Message) error {
	req := &proto.EchoReq{}
	err := protobuf.Unmarshal(msg.Payload, req)
	if err != nil {
		return err
	}
	glog.Infof(ctx, "recv echo req:%v", req)
	rsp := &proto.EchoAck{
		Code: 0,
		Msg:  req.Msg,
	}
	ep.SendMsg(rsp)
	return nil
}

func EchoServer() {
	// p, err := gxynet.NewNetwork("config/config.toml")
	// if err != nil {
	// 	glog.Error(ctx, "gxynet", zap.Namespace("new failed"), zap.Error(err))
	// 	return
	// }
	// if err := p.Start(context.Background(), &EchoEventHandler{}); err != nil {
	// 	glog.Error(ctx, "gxynet", zap.Namespace("start failed"), zap.Error(err))
	// 	return
	// }
}
