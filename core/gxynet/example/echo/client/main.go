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
	"sync"
	"time"

	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/example/echo/proto"
	"gserver/core/gxynet/message"

	"github.com/gogf/gf/v2/os/glog"
	"go.uber.org/zap"
	protobuf "google.golang.org/protobuf/proto"
)

var wg sync.WaitGroup
var ctx = context.Background()

func main() {
	EchoClient()
}

type EchoEventHandler struct {
	endpoint.BaseEventHandler
}

func (e *EchoEventHandler) OnOpen(conn endpoint.Endpoint) error {
	glog.Infof(ctx, "conn open, addr=%s", conn.Conn().RemoteAddr())
	// go run(conn)
	return nil
}

func (e *EchoEventHandler) OnClose(conn endpoint.Endpoint) {
	glog.Infof(ctx, "conn close, addr=%s", conn.Conn().RemoteAddr())
}

func (e *EchoEventHandler) OnMessage(ep endpoint.Endpoint, msg *message.Message) error {
	rsp := &proto.EchoAck{}
	err := protobuf.Unmarshal(msg.Payload, rsp)
	if err != nil {
		return err
	}
	glog.Infof(ctx, "recv message:%v", rsp)
	return nil
}

func run(sess endpoint.Endpoint) {
	var i int
	for {
		msg := &proto.EchoReq{
			Msg: fmt.Sprintf("hello %d", i),
		}
		if err := sess.SendMsg(msg); err != nil {
			glog.Error(ctx, "send error", zap.Error(err))
			break
		}
		i++
		time.Sleep(time.Second * 5)
	}
}

func EchoClient() {
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
