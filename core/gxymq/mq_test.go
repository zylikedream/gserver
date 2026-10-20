package gxymq

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// ========== NewMessageQueueApp ==========

func TestNewMessageQueueApp_Init(t *testing.T) {
	mq := NewMessageQueueApp()
	if mq.msgCh == nil || cap(mq.msgCh) != msgBufferSize {
		t.Fatalf("msgCh 容量 = %d, want %d", cap(mq.msgCh), msgBufferSize)
	}
	if len(mq.subs) != 0 {
		t.Fatalf("expected empty subs, got %d", len(mq.subs))
	}
	if mq.stopCh == nil {
		t.Fatal("expected non-nil stopCh")
	}
}

// ========== Subscribe ==========

func TestSubscribe_MultipleTopics(t *testing.T) {
	mq := NewMessageQueueApp()
	if err := mq.Subscribe(context.Background(), "t1", func(ctx context.Context, msg string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := mq.Subscribe(context.Background(), "t2", func(ctx context.Context, msg string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := mq.Subscribe(context.Background(), "t3", func(ctx context.Context, msg string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(mq.subs) != 3 {
		t.Fatalf("expected 3 subs, got %d", len(mq.subs))
	}
}

// TestSubscribe_RejectsDuplicate 重复订阅必须报错,而不是静默覆盖或重复建立。
// 为什么需要:redis 下重复订阅会让同一条消息被处理两次;pulsar 下第二次订阅会成为
// 同一共享订阅的第二个成员,把原消费者的一半消息抢走 —— 两者都不报错。
func TestSubscribe_RejectsDuplicate(t *testing.T) {
	ctx := context.Background()
	mq := NewMessageQueueApp()
	if err := mq.Subscribe(ctx, "dup", noopHandler); err != nil {
		t.Fatalf("首次订阅: %v", err)
	}
	if err := mq.Subscribe(ctx, "dup", noopHandler); err == nil {
		t.Fatal("重复订阅必须报错")
	}

	// 启动后的路径同样拒绝,且不会重复调用后端。
	q := &countingQueue{}
	mq.queue = q
	mq.started.Store(true)
	if err := mq.Subscribe(ctx, "late", noopHandler); err != nil {
		t.Fatalf("启动后首次订阅: %v", err)
	}
	if err := mq.Subscribe(ctx, "late", noopHandler); err == nil {
		t.Fatal("启动后重复订阅必须报错")
	}
	if q.subs != 1 {
		t.Fatalf("后端 Subscribe 调用 %d 次, want 1", q.subs)
	}
}

// ========== processMessages (with stop signal) ==========

// TestProcessMessages_DeliversToHandlerPerSubscription 每条消息必须交给它所属订阅的
// handler,不同 topic 的 handler 不能串。
// 为什么需要:分发时取错 handler 会让某个 topic 的订阅永远收不到消息,或收到别的 topic
// 的消息 —— 静默的消息黑洞/串台,发送方以为已投递。
func TestProcessMessages_DeliversToHandlerPerSubscription(t *testing.T) {
	mq := NewMessageQueueApp()
	ctx := context.Background()

	const topics = 3
	received := make([]string, topics)
	var mu sync.Mutex
	for p := range topics {
		topic := "topic_" + string(rune('a'+p))
		if err := mq.Subscribe(ctx, topic, func(ctx context.Context, msg string) error {
			mu.Lock()
			received[p] = msg
			mu.Unlock()
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	wg.Go(func() { _ = mq.processMessages(ctx) })
	defer func() { close(mq.stopCh); wg.Wait() }()

	for p := range topics {
		topic := "topic_" + string(rune('a'+p))
		mq.msgCh <- queuedMessage{Topic: topic, Data: topic + ":payload", Handler: mq.subs[topic].Handler}
	}

	for range 200 {
		mu.Lock()
		done := true
		for _, r := range received {
			if r == "" {
				done = false
			}
		}
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	for p := range topics {
		want := "topic_" + string(rune('a'+p)) + ":payload"
		if received[p] != want {
			t.Fatalf("第 %d 个订阅收到 %q, want %q(分发串台或丢失)", p, received[p], want)
		}
	}
}

// TestProcessMessages_HandlerErrorDoesNotStopLoop:handler 返回 error 时只记日志,循环必须
// 继续处理后续消息。
// 为什么需要:处理错误时若提前 return,一条坏消息会让该订阅的整个 actor 停摆——后续消息
// 全部积压在通道里(缓冲 1000)直到溢出,且不会有任何报错冒泡。
func TestProcessMessages_HandlerErrorDoesNotStopLoop(t *testing.T) {
	mq := NewMessageQueueApp()
	ctx := context.Background()

	var mu sync.Mutex
	var got []string
	if err := mq.Subscribe(ctx, "err", func(ctx context.Context, msg string) error {
		mu.Lock()
		got = append(got, msg)
		mu.Unlock()
		return errors.New("handler boom")
	}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Go(func() { _ = mq.processMessages(ctx) })
	defer func() { close(mq.stopCh); wg.Wait() }()

	for _, msg := range []string{"first", "second"} {
		mq.msgCh <- queuedMessage{Topic: "err", Data: msg, Handler: mq.subs["err"].Handler}
	}

	for range 100 {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n == 2 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	t.Fatalf("handler error stopped the loop, got %v", got)
}

// TestProcessMessages_Stop:stopCh 关闭后循环必须返回,不留后台 goroutine。
// 为什么需要:停机路径靠它退出;返回不了则进程关闭时挂起,或在重启后与新消费者
// 抢同一个通道里的残留消息。
func TestProcessMessages_Stop(t *testing.T) {
	mq := NewMessageQueueApp()
	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Go(func() {
		_ = mq.processMessages(ctx)
	})
	// Give it time to enter the select loop
	time.Sleep(10 * time.Millisecond)
	// Close stopCh to stop processing
	close(mq.stopCh)
	wg.Wait()
}

// ========== MessageQueue singleton ==========

func TestMessageQueue_Singleton(t *testing.T) {
	mq1 := MessageQueue()
	mq2 := MessageQueue()
	if mq1 != mq2 {
		t.Fatal("MessageQueue() should return the same instance")
	}
}

// ========== OnModStartAfter 启动契约 ==========

// countingQueue 记录后端被调用次数。
type countingQueue struct {
	starts int
	subs   int
}

func (q *countingQueue) Publish(context.Context, string, string) error { return nil }
func (q *countingQueue) Subscribe(context.Context, string, TopicHandler) error {
	q.subs++
	return nil
}
func (q *countingQueue) Start(context.Context) error { q.starts++; return nil }
func (q *countingQueue) Close(context.Context) error { return nil }

// TestOnModStartAfterStartsQueueOnce 后端只应 Start 一次。
// 原实现在 WorkerCount 循环之后又调了一次 Start(复制残留):对 redis 后端只是
// 重复赋值,对 pulsar 后端则是重复创建客户端并泄漏前一个。
func TestOnModStartAfterStartsQueueOnce(t *testing.T) {
	q := &countingQueue{}
	mq := &messageQueueApp{
		queue: q,
		subs: map[string]*SubInfo{
			"topic": {Topic: "topic", Handler: func(context.Context, string) error { return nil }},
		},
		config: &messageQueueConfig{WorkerCount: 0}, // 不起 worker,避免协程泄漏
		stopCh: make(chan struct{}),
	}

	if err := mq.OnModStartAfter(context.Background()); err != nil {
		t.Fatalf("OnModStartAfter: %v", err)
	}
	if q.starts != 1 {
		t.Fatalf("queue.Start 调用 %d 次, want 1", q.starts)
	}
	if q.subs != 1 {
		t.Fatalf("queue.Subscribe 调用 %d 次, want 1", q.subs)
	}
}

// TestPulsarStartCreatesClient Start 的契约:返回 nil 就意味着 client 已就绪。
// 原实现用包级 client 变量判空、且条件写反(`if client != nil`),于是 p.client 恒为
// nil 而 Start 仍返回 nil —— 之后 Publish/Subscribe/Close 都是 nil 接口方法调用,
// 直接 panic。URL 为空时 NewClient 立即返回错误,用它区分两种实现。
func TestPulsarStartCreatesClient(t *testing.T) {
	p := &PulsarMQ{config: &pulsarMQConfig{URL: ""}, stopCh: make(chan struct{})}

	err := p.Start(context.Background())
	if err == nil && p.client == nil {
		t.Fatal("Start 返回 nil 但 client 为 nil:后续 Publish/Subscribe/Close 会 panic")
	}
}

// TestOnModStopBeforeStopsConsumers 停机必须让消费者退出。
// 为什么需要:原来 OnModStopBefore 只停了后端订阅,worker 一直阻塞在
// reflect.Select 上——模块已"停止"却仍在消费,停机后到达的消息还会被处理。
func TestOnModStopBeforeStopsConsumers(t *testing.T) {
	mq := &messageQueueApp{
		queue:      &countingQueue{},
		subs:       map[string]*SubInfo{},
		subscribed: map[string]struct{}{},
		config:     &messageQueueConfig{WorkerCount: 1},
		stopCh:     make(chan struct{}),
	}

	var wg sync.WaitGroup
	wg.Go(func() { _ = mq.processMessages(context.Background()) })
	time.Sleep(10 * time.Millisecond) // 让 worker 进入 select

	if err := mq.OnModStopBefore(context.Background()); err != nil {
		t.Fatalf("OnModStopBefore: %v", err)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("OnModStopBefore 后 worker 未退出")
	}
}

// TestSubscribeAfterStartTakesEffect 启动完成后的订阅必须立即建立。
// 为什么需要:startSubscribe 只在 OnModStartAfter 跑一次;若晚来的 Subscribe 只写
// subs 不真正订阅,该主题永远不会收到消息且无任何报错。
func TestSubscribeAfterStartTakesEffect(t *testing.T) {
	q := &countingQueue{}
	mq := &messageQueueApp{
		queue:      q,
		subs:       map[string]*SubInfo{},
		subscribed: map[string]struct{}{},
		config:     &messageQueueConfig{WorkerCount: 0},
		stopCh:     make(chan struct{}),
	}
	mq.started.Store(true) // 模拟 OnModStartAfter 已跑完

	if err := mq.Subscribe(context.Background(), "late", noopHandler); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if q.subs != 1 {
		t.Fatalf("后端 Subscribe 调用 %d 次, want 1(晚订阅必须立即生效)", q.subs)
	}
	if _, ok := mq.subs["late"]; ok {
		t.Error("启动后不应再写 subs:那时它已无人读取,只会与遍历竞争")
	}
}

// TestSubscribeBeforeStartDefersToStartSubscribe 启动前的订阅只登记,
// 由 startSubscribe 统一建立,避免重复订阅。
func TestSubscribeBeforeStartDefersToStartSubscribe(t *testing.T) {
	q := &countingQueue{}
	mq := &messageQueueApp{
		queue:      q,
		subs:       map[string]*SubInfo{},
		subscribed: map[string]struct{}{},
		config:     &messageQueueConfig{WorkerCount: 0},
		stopCh:     make(chan struct{}),
	}

	if err := mq.Subscribe(context.Background(), "early", noopHandler); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if q.subs != 0 {
		t.Fatalf("后端 Subscribe 调用 %d 次, want 0(启动前只登记)", q.subs)
	}
	if _, ok := mq.subs["early"]; !ok {
		t.Fatal("启动前订阅必须登记到 subs")
	}

	if err := mq.OnModStartAfter(context.Background()); err != nil {
		t.Fatalf("OnModStartAfter: %v", err)
	}
	if q.subs != 1 {
		t.Fatalf("后端 Subscribe 调用 %d 次, want 1", q.subs)
	}
}
