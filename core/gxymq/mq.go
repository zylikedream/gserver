package gxymq

import (
	"context"
	"sync/atomic"

	"gserver/core/gxyapp"
	"gserver/core/gxylog"
	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/frame/g"
)

// MessageQueueType 消息队列类型
type MessageQueueType string

// msgBufferSize 是待处理消息队列的容量:worker 忙不过来时先缓冲,满了则反压到
// 订阅回调(进而阻塞该主题的接收协程)。
const msgBufferSize = 1000

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
	queue  IMessageQueue
	msgCh  chan queuedMessage
	subs   map[string]*SubInfo
	config *messageQueueConfig
	// stopCh 关闭后 worker 退出
	stopCh chan struct{}
	// started 标记 OnModStartAfter 是否已跑完,决定 Subscribe 是否需要即时建立订阅
	started atomic.Bool
}

var mqApp *messageQueueApp

func MessageQueue() *messageQueueApp {
	return mqApp
}

// NewMessageQueueApp 创建消息队列实例，延迟初始化到 OnModInit
func NewMessageQueueApp() *messageQueueApp {
	mqApp = &messageQueueApp{
		msgCh:  make(chan queuedMessage, msgBufferSize),
		subs:   make(map[string]*SubInfo),
		stopCh: make(chan struct{}),
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
		go func() { _ = mq.processMessages(ctx) }()
	}
	mq.started.Store(true)
	return nil
}

// Publish 发布消息到指定主题
func (mq *messageQueueApp) Publish(ctx context.Context, topic string, msg string) error {
	// 直接使用Redis的PUBLISH命令发布消息
	return mq.queue.Publish(ctx, topic, msg)
}

// Subscribe 订阅指定主题。
// 启动完成之后再订阅必须立即建立订阅:startSubscribe 只在 OnModStartAfter 跑一次,
// 若此时仅写入 subs,该订阅会静默失效(sub 记下了但没人消费)。启动完成后的路径
// 不写 subs —— 那时 subs 已无人再读,写它反而与 startSubscribe 的遍历构成竞争。
func (mq *messageQueueApp) Subscribe(ctx context.Context, topic string, handler func(ctx context.Context, msg string) error) error {
	sub := &SubInfo{
		Topic:   topic,
		Handler: handler,
	}
	if mq.started.Load() {
		return mq.doSubscribe(ctx, sub)
	}
	mq.subs[topic] = sub
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
	if err := mq.queue.Subscribe(ctx, subInfo.Topic, func(ctx context.Context, msg string) error {
		mq.msgCh <- queuedMessage{
			Topic:   subInfo.Topic,
			Data:    msg,
			Handler: subInfo.Handler,
		}
		return nil
	}); err != nil {
		return err
	}

	gxylog.Info(ctx, "Subscribing to topic", gxylog.Str("topic", subInfo.Topic))

	return nil
}

// processMessages 消费队列,直到停机。
func (mq *messageQueueApp) processMessages(ctx context.Context) error {
	for {
		select {
		case <-mq.stopCh:
			return nil
		case msg := <-mq.msgCh:
			if err := msg.Handler(ctx, msg.Data); err != nil {
				gxylog.Error(ctx, "Failed to process message", gxylog.Str("data", msg.Data), gxylog.Str("topic", msg.Topic), gxylog.Err(err))
			}
		}
	}
}

// OnModStopBefore 停机:先停后端订阅(不再产生新消息),再关闭停止通道让
// worker 退出。原来只停了后端,worker 一直阻塞在 reflect.Select 上永不退出 ——
// "模块停了但消费者还在跑",且已缓冲在 msgCh 里的消息仍会被处理。
func (mq *messageQueueApp) OnModStopBefore(ctx context.Context) error {
	_ = mq.queue.Close(ctx)
	close(mq.stopCh)
	gxylog.Info(ctx, "message queue stopped")
	return nil
}
