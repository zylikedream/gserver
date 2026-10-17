package gxymq

import (
	"context"

	"gserver/core/gxylog"
	"gserver/core/gxyredis"

	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/redis/go-redis/v9"
)

// 移除了消息级优先级相关结构体

// RedisMQ Redis消息队列实现
type RedisMQ struct {
	// Redis客户端
	client gxyredis.Client
	stopCh chan struct{}
}

// NewRedisMQ 创建Redis消息队列实例
func NewRedisMQ(_ *gcfg.Config) (*RedisMQ, error) {
	mq := &RedisMQ{
		stopCh: make(chan struct{}),
	}
	return mq, nil
}

func (mq *RedisMQ) Start(ctx context.Context) error {
	// 初始化Redis客户端
	mq.client = gxyredis.Redis()
	return nil
}

// Publish 发布消息到指定主题
func (mq *RedisMQ) Publish(ctx context.Context, topic string, msg string) error {
	// 直接使用Redis的PUBLISH命令发布消息
	if err := mq.client.Publish(ctx, topic, msg).Err(); err != nil {
		gxylog.Error(ctx, "Redis publish message failed", gxylog.Str("topic", topic), gxylog.Err(err))
		return err
	}
	return nil
}

// Subscribe 订阅指定主题，基于topic优先级处理消息
func (mq *RedisMQ) Subscribe(ctx context.Context, topic string, handler TopicHandler) error {
	subscriber := mq.client.Subscribe(ctx, topic)
	go func() {
		defer func() {
			if err := subscriber.Close(); err != nil {
				gxylog.Error(ctx, "Close subscriber failed", gxylog.Err(err))
			}
			gxylog.Info(ctx, "Redis subscriber closed", gxylog.Str("topic", topic))
		}()
		mq.consume(ctx, topic, subscriber.Channel(), handler)
	}()
	return nil
}

// consume 把收到的消息交给 handler,在停机或消息通道关闭时返回。
// 通道关闭必须当成终止条件:go-redis 在连接池关闭时会 close 通道,继续 receive
// 只会立刻拿到零值 —— 若按"空消息"跳过就是 100% CPU 忙等。
func (mq *RedisMQ) consume(ctx context.Context, topic string, ch <-chan *redis.Message, handler TopicHandler) {
	for {
		select {
		case <-mq.stopCh:
			return
		case msg, ok := <-ch:
			if !ok {
				gxylog.Info(ctx, "Redis subscriber channel closed", gxylog.Str("topic", topic))
				return
			}
			if err := handler(ctx, msg.Payload); err != nil {
				gxylog.Error(ctx, "Failed to handle message", gxylog.Str("topic", topic), gxylog.Err(err))
			}
		}
	}
}

func (mq *RedisMQ) Close(ctx context.Context) error {
	close(mq.stopCh)
	return nil
}
