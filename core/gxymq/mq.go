package gxymq

import (
	"context"

	"github.com/gogf/gf/v2/os/glog"
)

// MessageQueueType 消息队列类型
type MessageQueueType string

const (
	// MessageQueuePulsar Pulsar消息队列
	MQTypePulsar MessageQueueType = "pulsar"
	// MQTypeRedis Redis消息队列
	MQTypeRedis MessageQueueType = "redis"
)

type IMessageQueue interface {
	Publish(ctx context.Context, topic string, msg string) error
	Subscribe(ctx context.Context, topic string, handler TopicHandler) error
	Start(ctx context.Context) error
	Close(ctx context.Context) error
}

type MessageQueue struct {
	queue      IMessageQueue
	priorityCh []chan PriorityData
	subs       map[string]*SubInfo
	// 是否已关闭
	closed bool
	stopCh chan struct{}
}

// NewMessageQueue 创建默认的消息队列实例
// 默认使用Redis实现，如果Redis不可用则尝试使用Pulsar
func NewMessageQueue(t MessageQueueType, config string) (*MessageQueue, error) {
	// 首先尝试创建Redis消息队列
	mq := &MessageQueue{
		priorityCh: make([]chan PriorityData, TOPIC_PRIORITY_MAX),
	}
	for i := range int(TOPIC_PRIORITY_MAX) {
		mq.priorityCh[i] = make(chan PriorityData, 1000)
	}
	var err error
	switch t {
	case MQTypePulsar:
		mq.queue, err = NewPulsarMQ(config)
	case MQTypeRedis:
		mq.queue, err = NewRedisMQ(config)
	default:
		mq.queue, err = NewRedisMQ(config)
	}
	if err != nil {
		return nil, err
	}
	return mq, nil
}

func (mq *MessageQueue) Start(ctx context.Context) error {
	if err := mq.queue.Start(ctx); err != nil {
		return err
	}
	if err := mq.startSubscribe(ctx); err != nil {
		return err
	}
	go mq.processMessages(ctx)
	if err := mq.queue.Start(ctx); err != nil {
		return err
	}
	return nil
}

// Publish 发布消息到指定主题
func (mq *MessageQueue) Publish(ctx context.Context, topic string, msg string) error {
	// 直接使用Redis的PUBLISH命令发布消息
	return mq.queue.Publish(ctx, topic, msg)
}

// Subscribe 订阅指定主题，基于topic优先级处理消息
func (mq *MessageQueue) Subscribe(ctx context.Context, topic string, handler func(ctx context.Context, msg string) error, priorityArg ...MessagePriority) error {
	priority := TOPIC_PRIORITY_NORMAL
	if len(priorityArg) > 0 {
		priority = priorityArg[0]
	}
	mq.subs[topic] = &SubInfo{
		Topic:    topic,
		Priority: priority,
		Handler:  handler,
	}
	return nil
}

func (mq *MessageQueue) startSubscribe(ctx context.Context) error {
	for _, sub := range mq.subs {
		if err := mq.doSubscribe(ctx, sub); err != nil {
			return err
		}
	}
	return nil
}
func (mq *MessageQueue) doSubscribe(ctx context.Context, subInfo *SubInfo) error {
	// 创建Redis订阅者
	mq.queue.Subscribe(ctx, subInfo.Topic, func(ctx context.Context, msg string) error {
		// 发送消息到通道
		mq.priorityCh[subInfo.Priority] <- PriorityData{
			Topic:   subInfo.Topic,
			Data:    msg,
			Handler: subInfo.Handler,
		}
		return nil
	})

	// 初始化消息通道
	glog.Infof(ctx, "Subscribing to topic: %s with priority: %d", subInfo.Topic, subInfo.Priority)

	return nil
}

// processMessages 处理消息
func (mq *MessageQueue) processMessages(ctx context.Context) error {
	// 获取topic优先级，用于日志记录
	for {
	pri:
		select {
		case <-mq.stopCh:
			return nil
		default:
			// 检查消息通道
			for _, priChan := range mq.priorityCh {
				select {
				case msg := <-priChan:
					// 处理消息
					if err := msg.Handler(ctx, msg.Data); err != nil {
						glog.Errorf(ctx, "Failed to process message: %v, topic: %s, err: %+v", msg.Data, msg.Topic, err)
					}
					break pri
				default:
				}
			}
		}
	}
}

// Close 关闭消息队列，释放所有资源
func (mq *MessageQueue) Close(ctx context.Context) error {
	if mq.closed {
		return nil // 已经关闭，直接返回
	}
	for _, sub := range mq.subs {
		if err := sub.subscriber.Close(); err != nil {
			glog.Errorf(ctx, "Close subscriber failed: %v, topic: %s", err, sub.Topic)
		}
	}
	mq.closed = true
	glog.Infof(ctx, "Redis message queue closed")
	return nil
}
