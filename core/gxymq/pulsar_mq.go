package gxymq

import (
	"context"
	"gserver/util"

	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/glog"
)

// PulsarMQ pulsar消息队列实现
type PulsarMQ struct {
	// Pulsar客户端
	config *pulsarMQConfig
	client pulsar.Client
	// 停止通道
	stopCh chan struct{}
}

type pulsarMQConfig struct {
	URL string `toml:"url"`
}

// NewPulsarMQ 创建Pulsar消息队列实例
func NewPulsarMQ(config string) (*PulsarMQ, error) {
	// 创建Pulsar客户端
	conf := &pulsarMQConfig{}
	cfg := gcfg.Instance(config)
	if err := util.CfgUnmarshalKey(context.Background(), cfg, "mq.pulsar", conf); err != nil {
		return nil, err
	}
	return &PulsarMQ{
		config: conf,
		stopCh: make(chan struct{}),
	}, nil
}

func (p *PulsarMQ) Start(ctx context.Context) error {
	clientOptions := pulsar.ClientOptions{
		URL: p.config.URL,
	}

	client, err := pulsar.NewClient(clientOptions)
	if err != nil {
		return gerror.Wrap(err, "Pulsar client Start failed")
	}
	p.client = client
	return nil
}

// Publish 发布消息到指定主题
func (p *PulsarMQ) Publish(ctx context.Context, topic string, msg string) error {
	// 创建生产者
	producer, err := p.client.CreateProducer(pulsar.ProducerOptions{
		Topic: topic,
	})
	if err != nil {
		glog.Errorf(ctx, "Create pulsar producer failed: %v, topic: %s", err, topic)
		return err
	}
	defer producer.Close()

	// 发送消息
	_, err = producer.Send(ctx, &pulsar.ProducerMessage{
		Payload: []byte(msg),
	})
	if err != nil {
		glog.Errorf(ctx, "Pulsar publish message failed: %v, topic: %s", err, topic)
		return err
	}

	return nil
}

// Subscribe 订阅指定主题
func (p *PulsarMQ) Subscribe(ctx context.Context, topic string, handler TopicHandler) error {
	// 创建消费者
	consumer, err := p.client.Subscribe(pulsar.ConsumerOptions{
		Topic:            topic,
		SubscriptionName: "sub-" + topic,
	})
	if err != nil {
		glog.Errorf(ctx, "Pulsar subscribe failed: %v, topic: %s", err, topic)
		return err
	}

	// 启动消费协程
	go func() {
		defer func() {
			consumer.Close() // Close方法没有返回值
			glog.Infof(ctx, "Pulsar consumer closed: %s", topic)
		}()

		for {
			select {
			case <-p.stopCh:
				return
			default:
				// 接收消息
				msg, err := consumer.Receive(ctx)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					glog.Errorf(ctx, "Failed to receive message: %v, topic: %s", err, topic)
					continue
				}

				// 处理消息
				if err := handler(ctx, string(msg.Payload())); err != nil {
					glog.Errorf(ctx, "Failed to handle message: %v, topic: %s", err, topic)
					// 处理失败，重试
					consumer.Nack(msg)
				} else {
					// 处理成功，确认消息
					consumer.Ack(msg)
				}
			}
		}
	}()

	return nil
}

// Close 关闭消息队列，释放所有资源
func (p *PulsarMQ) Close(ctx context.Context) error {
	// 关闭Pulsar客户端
	p.client.Close()
	// 关闭停止通道
	close(p.stopCh)
	glog.Infof(ctx, "Pulsar message queue closed")
	return nil
}
