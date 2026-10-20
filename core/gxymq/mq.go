package gxymq

import (
	"context"
	"sync"
	"sync/atomic"

	"gserver/core/gxyapp"
	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxyutil"

	"github.com/cockroachdb/errors"
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

// IMessageQueue 是消息队列后端。
//
// **投递语义是 at-most-once**,两个后端都如此,原因不同:
//   - redis:pub/sub 没有确认与重投,错过即丢;
//   - pulsar:app 层传给后端的回调只负责入队,入队成功即 Ack(pulsar_mq.go),
//     真正的业务处理在其后的 worker 里 —— 崩溃或处理失败都不会重投。
//
// 因此 handler 失败只记 mq_handler_errors_total 并打日志(见 processMessages),
// 停机时 worker 直接退出、不保证排空 msgCh。需要"不丢"的消息不要依赖本模块的
// 重试,应在业务侧持久化 + 幂等重放。
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
	config *messageQueueConfig
	// stopCh 关闭后 worker 退出
	stopCh chan struct{}
	// started 表示 OnModStartAfter 的订阅快照已完成;切换与快照都在 subsMu 内,
	// 因此不存在"记下了却没建"的窗口(见 OnModStartAfter)。
	started atomic.Bool

	// subsMu 保护 subs 与 subscribed。
	//
	// 启动前的 Subscribe 只写 subs(由 OnModStartAfter 统一建立);启动后必须立即
	// 建立订阅。重复订阅同一 topic 必须拒绝:redis 下会重复处理,pulsar 下会成为
	// 同一共享订阅的第二个成员、抢走原消费者的一半消息——两者都不报错。
	subsMu     sync.Mutex
	subs       map[string]*SubInfo
	subscribed map[string]struct{}
}

var mqApp *messageQueueApp

func MessageQueue() *messageQueueApp {
	return mqApp
}

// NewMessageQueueApp 创建消息队列实例，延迟初始化到 OnModInit
func NewMessageQueueApp() *messageQueueApp {
	mqApp = &messageQueueApp{
		msgCh:      make(chan queuedMessage, msgBufferSize),
		subs:       make(map[string]*SubInfo),
		subscribed: make(map[string]struct{}),
		stopCh:     make(chan struct{}),
	}
	gxymetrics.MQQueueCapacity.Set(float64(cap(mqApp.msgCh)))
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

	// 在锁内完成"启动完成"的切换与待建订阅的快照:此后 Subscribe 一律立即建立,
	// 不会再出现"记进了 subs 却没人建订阅"的静默失效。
	mq.subsMu.Lock()
	mq.started.Store(true)
	pending := make([]*SubInfo, 0, len(mq.subs))
	for _, sub := range mq.subs {
		pending = append(pending, sub)
	}
	mq.subsMu.Unlock()

	for _, sub := range pending {
		if err := mq.doSubscribe(ctx, sub); err != nil {
			return err
		}
	}

	for range mq.config.WorkerCount {
		go func() { _ = mq.processMessages(ctx) }()
	}
	return nil
}

// Publish 发布消息到指定主题
func (mq *messageQueueApp) Publish(ctx context.Context, topic string, msg string) error {
	return mq.queue.Publish(ctx, topic, msg)
}

// Subscribe 订阅指定主题。
//
// 启动前只登记,由 OnModStartAfter 统一建立;启动后立即建立。重复订阅同一 topic
// 返回错误(理由见 subsMu 的注释)。
func (mq *messageQueueApp) Subscribe(ctx context.Context, topic string, handler func(ctx context.Context, msg string) error) error {
	mq.subsMu.Lock()
	if _, ok := mq.subscribed[topic]; ok {
		mq.subsMu.Unlock()
		return errors.Newf("topic %s already subscribed", topic)
	}
	mq.subscribed[topic] = struct{}{}
	sub := &SubInfo{
		Topic:   topic,
		Handler: handler,
	}
	if !mq.started.Load() {
		mq.subs[topic] = sub
		mq.subsMu.Unlock()
		return nil
	}
	mq.subsMu.Unlock()
	return mq.doSubscribe(ctx, sub)
}

func (mq *messageQueueApp) doSubscribe(ctx context.Context, subInfo *SubInfo) error {
	// 创建Redis订阅者
	if err := mq.queue.Subscribe(ctx, subInfo.Topic, func(ctx context.Context, msg string) error {
		mq.msgCh <- queuedMessage{
			Topic:   subInfo.Topic,
			Data:    msg,
			Handler: subInfo.Handler,
		}
		gxymetrics.MQQueueDepth.Set(float64(len(mq.msgCh)))
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
			gxymetrics.MQQueueDepth.Set(float64(len(mq.msgCh)))
			if err := msg.Handler(ctx, msg.Data); err != nil {
				// 处理失败不重试(at-most-once,见 OnModStopBefore 的说明):
				// 计数 + 记日志是唯一的可观测手段。
				gxymetrics.MQHandlerErrors.Inc()
				gxylog.Error(ctx, "Failed to process message", gxylog.Str("data", msg.Data), gxylog.Str("topic", msg.Topic), gxylog.Err(err))
			}
		}
	}
}

// OnModStopBefore 停机:先停后端订阅(不再产生新消息),再关闭停止通道让
// worker 退出。原来只停了后端,worker 一直阻塞在 reflect.Select 上永不退出 ——
// "模块停了但消费者还在跑",且已缓冲在 msgCh 里的消息仍会被处理。
//
// 注意投递语义是 at-most-once:worker 在此退出,**不保证**排空 msgCh 里已缓冲
// 的消息;需要"停机前处理完"的调用方必须自己保证幂等或另做持久化。
func (mq *messageQueueApp) OnModStopBefore(ctx context.Context) error {
	_ = mq.queue.Close(ctx)
	close(mq.stopCh)
	gxylog.Info(ctx, "message queue stopped")
	return nil
}
