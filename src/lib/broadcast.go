package lib

import (
	"context"
	"fmt"

	"gserver/core/gxymodule"
	"gserver/core/gxymq"

	"github.com/gogf/gf/v2/encoding/gjson"
)

const (
	BroadCastTypeSystemMsg    = "system_msg"    // 系统消息
	BroadCastTypeReloadConfig = "reload_config" // 刷新配置消息(todo)
)

// BroadcastMsg 广播消息结构体
type BroadcastMsg struct {
	MsgType string
	Data    string
}

// IBroadcastHandler 广播消息处理接口，嵌入方需实现
type BroadcastHandler = func(ctx context.Context, topic string, msg *BroadcastMsg) *BroadcastMsg

// Broadcast 广播模块
// 嵌入方在 OnModInit 中调用 Init 设置服务名和处理器，然后 AddModule 注册到生命周期。
// OnModStart 时自动订阅两个 topic：
//   - global:{ServiceName}:broadcast  — 本服务广播
//   - global:all:broadcast           — 全局广播
type Broadcast struct {
	gxymodule.ModuleBase
	handlers    []BroadcastHandler
	ServiceName string
	topics      []string
}

// 一些通用的消息处理函数, 避免重复写代码
func CommonMsgHandler(ctx context.Context, topic string, msg *BroadcastMsg) *BroadcastMsg {
	switch msg.MsgType {
	case BroadCastTypeReloadConfig:
		// 系统消息，直接返回
		return nil
	}
	return msg
}

func NewBroadcast(serviceName string, handler BroadcastHandler) *Broadcast {
	b := &Broadcast{
		handlers: []BroadcastHandler{CommonMsgHandler},
	}
	if handler != nil {
		b.handlers = append(b.handlers, handler)
	}
	b.ServiceName = serviceName
	b.topics = []string{
		"global:" + serviceName + ":broadcast",
		"global:all:broadcast",
	}
	return b
}

func (b *Broadcast) OnModStart(ctx context.Context) error {
	for _, topic := range b.topics {
		t := topic
		if err := gxymq.MessageQueue().Subscribe(ctx, t, func(c context.Context, msg string) error {
			bMsg := &BroadcastMsg{}
			if err := gjson.Unmarshal([]byte(msg), bMsg); err != nil {
				return fmt.Errorf("broadcast msg json scan error: %w", err)
			}
			for _, handler := range b.handlers {
				bMsg = handler(ctx, t, bMsg)
				if bMsg == nil {
					break
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("broadcast subscribe %s: %w", t, err)
		}
	}
	return nil
}

// Publish 向本服务的广播 topic 发送消息
func Publish(ctx context.Context, serviceName string, msg *BroadcastMsg) error {
	msgStr, err := gjson.EncodeString(msg)
	if err != nil {
		return fmt.Errorf("broadcast msg json scan error: %w", err)
	}
	return gxymq.MessageQueue().Publish(ctx, "global:"+serviceName+":broadcast", msgStr)
}

// PublishToAll 向全局广播 topic 发送消息（所有服务都会收到）
func PublishToAll(ctx context.Context, msg *BroadcastMsg) error {
	msgStr, err := gjson.EncodeString(msg)
	if err != nil {
		return fmt.Errorf("broadcast msg json scan error: %w", err)
	}
	return gxymq.MessageQueue().Publish(ctx, "global:all:broadcast", msgStr)
}
