package gxymq

import (
	"context"
	"sync"
	"testing"

	"github.com/apache/pulsar-client-go/pulsar"
)

// fakeProducer 只覆写 Send/Close,其余方法走嵌入接口(测试不调用)。
type fakeProducer struct {
	pulsar.Producer
	mu   sync.Mutex
	sent []string
	// topic 仅用于断言缓存按 topic 分组
	name string
}

func (p *fakeProducer) Send(_ context.Context, msg *pulsar.ProducerMessage) (pulsar.MessageID, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, string(msg.Payload))
	return nil, nil
}

func (p *fakeProducer) Close() {}

func (p *fakeProducer) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sent)
}

// fakePulsarClient 只覆写 CreateProducer,记录每个 topic 各创建了几次。
type fakePulsarClient struct {
	pulsar.Client
	mu       sync.Mutex
	created  map[string]int
	producer *fakeProducer
}

func (c *fakePulsarClient) CreateProducer(opts pulsar.ProducerOptions) (pulsar.Producer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.created[opts.Topic]++
	return c.producer, nil
}

// Close 必须覆写:PulsarMQ.Close 会调用它,嵌入的 nil 接口会 panic。
func (c *fakePulsarClient) Close() {}

func (c *fakePulsarClient) createdCount(topic string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.created[topic]
}

func newPulsarTestMQ() (*PulsarMQ, *fakePulsarClient, *fakeProducer) {
	producer := &fakeProducer{}
	client := &fakePulsarClient{created: make(map[string]int), producer: producer}
	return &PulsarMQ{
		config:    &pulsarMQConfig{},
		client:    client,
		stopCh:    make(chan struct{}),
		producers: make(map[string]pulsar.Producer),
	}, client, producer
}

// TestPulsarPublishReusesProducer 同一 topic 重复发布只应创建一个 producer。
// 为什么需要:producer 是有状态的长连接(建分区队列、与 broker 握手),原实现每条
// 消息创建再 defer Close,开销远大于发送本身。
func TestPulsarPublishReusesProducer(t *testing.T) {
	mq, client, producer := newPulsarTestMQ()
	ctx := context.Background()

	for i := range 5 {
		if err := mq.Publish(ctx, "global:role:broadcast", string(rune('a'+i))); err != nil {
			t.Fatalf("Publish #%d: %v", i, err)
		}
	}

	if got := client.createdCount("global:role:broadcast"); got != 1 {
		t.Fatalf("CreateProducer 调用 %d 次, want 1(必须复用)", got)
	}
	if got := producer.count(); got != 5 {
		t.Fatalf("Send 调用 %d 次, want 5", got)
	}
}

// TestPulsarPublishCachesPerTopic 缓存按 topic 分组,不同 topic 各自一个 producer。
func TestPulsarPublishCachesPerTopic(t *testing.T) {
	mq, client, _ := newPulsarTestMQ()
	ctx := context.Background()

	for _, topic := range []string{"t1", "t2", "t1", "t2", "t2"} {
		if err := mq.Publish(ctx, topic, "m"); err != nil {
			t.Fatalf("Publish %s: %v", topic, err)
		}
	}

	if got := client.createdCount("t1"); got != 1 {
		t.Errorf("t1 CreateProducer = %d, want 1", got)
	}
	if got := client.createdCount("t2"); got != 1 {
		t.Errorf("t2 CreateProducer = %d, want 1", got)
	}
}

// TestPulsarCloseReleasesProducers 停机必须释放缓存的生产者。
func TestPulsarCloseReleasesProducers(t *testing.T) {
	mq, _, _ := newPulsarTestMQ()
	ctx := context.Background()

	if err := mq.Publish(ctx, "t1", "m"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(mq.producers) != 1 {
		t.Fatalf("producers = %d, want 1", len(mq.producers))
	}

	if err := mq.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(mq.producers) != 0 {
		t.Fatalf("Close 后 producers = %d, want 0", len(mq.producers))
	}
}
