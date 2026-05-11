package main

import (
	"context"

	"gserver/core/gxylog"
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/example/echo/proto"
	"gserver/core/gxynet/message"

	protobuf "google.golang.org/protobuf/proto"
)

var ctx = context.Background()

func init() {
	var err error
	if err != nil {
		gxylog.Fatal(ctx, "NewMessageCodec failed", gxylog.Err(err))
		return
	}
}

func main() {
	EchoServer()
}

type EchoEventHandler struct {
	endpoint.BaseEventHandler
}

func (e *EchoEventHandler) OnOpen(ep endpoint.Endpoint) error {
	gxylog.Info(ctx, "conn open", gxylog.Str("addr", ep.Conn().RemoteAddr().String()))
	return nil
}

func (e *EchoEventHandler) OnClose(ep endpoint.Endpoint) {
	gxylog.Info(ctx, "conn close", gxylog.Str("addr", ep.Conn().RemoteAddr().String()))
}

func (e *EchoEventHandler) OnMessage(ep endpoint.Endpoint, msg *message.Message) error {
	req := &proto.EchoReq{}
	err := protobuf.Unmarshal(msg.Payload, req)
	if err != nil {
		return err
	}
	gxylog.Info(ctx, "recv echo req", gxylog.Any("req", req))
	rsp := &proto.EchoAck{
		Code: 0,
		Msg:  req.Msg,
	}
	SendMsg(ep, rsp)
	return nil
}

func SendMsg(ep endpoint.Endpoint, msg any) error {
	return ep.SendMsg(msg)
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
