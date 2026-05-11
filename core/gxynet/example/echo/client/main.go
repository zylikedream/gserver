/*
 * @Author: your name
 * @Date: 2021-11-04 17:39:40
 * @LastEditTime: 2021-11-05 15:58:53
 * @LastEditors: Please set LastEditors
 * @Description: In User Settings Edit
 * @FilePath: /components/gxynet/example/echo/client/config/main.go
 */
package main

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/example/echo/proto"
	"gserver/core/gxynet/message"

	protobuf "google.golang.org/protobuf/proto"
)

var ctx = context.Background()

func main() {
	EchoClient()
}

func init() {
}

type EchoEventHandler struct {
	endpoint.BaseEventHandler
}

func (e *EchoEventHandler) OnOpen(conn endpoint.Endpoint) error {
	gxylog.Info(ctx, "conn open", gxylog.Str("addr", conn.Conn().RemoteAddr().String()))
	go run(conn)
	return nil
}

func (e *EchoEventHandler) OnClose(conn endpoint.Endpoint) {
	gxylog.Info(ctx, "conn close", gxylog.Str("addr", conn.Conn().RemoteAddr().String()))
}

func (e *EchoEventHandler) OnMessage(ep endpoint.Endpoint, msg *message.Message) error {
	rsp := &proto.EchoAck{}
	err := protobuf.Unmarshal(msg.Payload, rsp)
	if err != nil {
		return err
	}
	gxylog.Info(ctx, "recv message", gxylog.Any("rsp", rsp))
	return nil
}

func run(sess endpoint.Endpoint) {
	var i int
	for {
		msg := &proto.EchoReq{
			Msg: fmt.Sprintf("hello %d", i),
		}
		if err := SendMsg(sess, msg); err != nil {
			gxylog.Error(ctx, "send error", gxylog.Err(err))
			break
		}
		i++
		time.Sleep(time.Second * 5)
	}
}

func SendMsg(ep endpoint.Endpoint, msg any) error {
	return ep.SendMsg(msg)
}

func EchoClient() {
	// p, err := gxynet.NewNetwork("config/config.toml")
	// if err != nil {
	// 	gxylog.Error(ctx, "gxynet", gxylog.Err(err))
	// 	return
	// }
	// if err := p.Start(context.Background(), &EchoEventHandler{}); err != nil {
	// 	glog.Error(ctx, "gxynet", zap.Namespace("start failed"), zap.Error(err))
	// 	return
	// }
}
