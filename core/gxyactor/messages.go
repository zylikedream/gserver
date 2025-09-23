package gxyactor

import (
	"gserver/core/gxynet/message"

	"ergo.services/ergo/net/edf"
)

// var codec message.IMessageCodec

func init() {
	// codec, _ = message.NewMessageCodec(message.MESSAGE_PROTOBUF)
	edf.RegisterTypeOf(message.Message{})
	edf.RegisterTypeOf(ActorMessage{})
}

// 消息类型常量
const (
	MsgSystem    = "system"
	MsgClientReq = "clientReq"
	MsgServerRsp = "serverRsp"
	MsgTimer     = "timer"
)

type ActorMessage struct {
	Name string `json:"name"` // 消息名称
	Data any    `json:"data"` // 消息数据
}

func NewActorMessage(name string, data any) *ActorMessage {
	return &ActorMessage{
		Name: name,
		Data: data,
	}
}
