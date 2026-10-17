package gxymq

import (
	"context"
)

// TopicHandler 处理一条主题消息,返回 error 表示处理失败(由调用方记日志)。
type TopicHandler func(ctx context.Context, msg string) error

// SubInfo 一次订阅:主题与处理函数。
type SubInfo struct {
	Topic   string
	Handler TopicHandler
}

// queuedMessage 投递给 worker 的一条消息。
type queuedMessage struct {
	Topic   string
	Data    string
	Handler TopicHandler
}
