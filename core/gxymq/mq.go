package gxymq

import (
	"context"
	"gserver/core/gxyapp"
	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/frame/g"
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

type messageQueueConfig struct {
	Type        MessageQueueType `json:"type"`
	WorkerCount int              `json:"worker_count"`
}

type messageQueueApp struct {
	gxyapp.App
	queue      IMessageQueue
	priorityCh []chan PriorityData
	subs       map[string]*SubInfo
	config     *messageQueueConfig
	// 是否已关闭
	closed bool
	stopCh chan struct{}
}

var mqApp *messageQueueApp

func MessageQueue() *messageQueueApp {
	return mqApp
}

// NewMessageQueueApp 创建消息队列实例，延迟初始化到 OnModInit
func NewMessageQueueApp() *messageQueueApp {
	mqApp = &messageQueueApp{
		priorityCh: make([]chan PriorityData, TOPIC_PRIORITY_MAX),
		subs:       make(map[string]*SubInfo),
		stopCh:     make(chan struct{}),
	}
	for i := range int(TOPIC_PRIORITY_MAX) {
		mqApp.priorityCh[i] = make(chan PriorityData, 1000)
	}
	return mqApp
}

func (mq *messageQueueApp) OnModInit(ctx context.Context) error {
	conf := &messageQueueConfig{
		Type:        MQTypeRedis,
		WorkerCount: 3,
	}
	if err := gxyutil.CfgUnmarshalKey(ctx, g.Cfg(), "mq", conf); err != nil {
		return err
	}
	mq.config = conf

	var err error
	var queue IMessageQueue
	cfg := g.Cfg()
	switch conf.Type {
	case MQTypePulsar:
		queue, err = NewPulsarMQ(cfg)
	case MQTypeRedis:
		queue, err = NewRedisMQ(cfg)
	default:
		queue, err = NewRedisMQ(cfg)
	}
	if err != nil {
		return err
	}
	mq.queue = queue
	return nil
}

func (mq *messageQueueApp) OnModStartAfter(ctx context.Context) error {
	if err := mq.queue.Start(ctx); err != nil {
		return err
	}
	if err := mq.startSubscribe(ctx); err != nil {
		return err
	}
	for range mq.config.WorkerCount {
		go mq.processMessages(ctx)
	}
	if err := mq.queue.Start(ctx); err != nil {
		return err
	}
	return nil
}

// Publish 发布消息到指定主题
func (mq *messageQueueApp) Publish(ctx context.Context, topic string, msg string) error {
	// 直接使用Redis的PUBLISH命令发布消息
	return mq.queue.Publish(ctx, topic, msg)
}

// Subscribe 订阅指定主题，基于topic优先级处理消息
func (mq *messageQueueApp) Subscribe(ctx context.Context, topic string, handler func(ctx context.Context, msg string) error, priorityArg ...MessagePriority) error {
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

func (mq *messageQueueApp) startSubscribe(ctx context.Context) error {
	for _, sub := range mq.subs {
		if err := mq.doSubscribe(ctx, sub); err != nil {
			return err
		}
	}
	return nil
}
func (mq *messageQueueApp) doSubscribe(ctx context.Context, subInfo *SubInfo) error {
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
func (mq *messageQueueApp) processMessages(ctx context.Context) error {
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
func (mq *messageQueueApp) Close(ctx context.Context) error {
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
