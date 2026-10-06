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
	if len(mq.priorityCh) != int(TOPIC_PRIORITY_MAX) {
		t.Fatalf("expected %d priority channels, got %d", TOPIC_PRIORITY_MAX, len(mq.priorityCh))
	}
	if len(mq.subs) != 0 {
		t.Fatalf("expected empty subs, got %d", len(mq.subs))
	}
	if mq.stopCh == nil {
		t.Fatal("expected non-nil stopCh")
	}
}

// ========== Subscribe ==========

// TestSubscribe_Priority 覆盖「不传优先级→NORMAL」与显式指定两种基准,合成一张表:
// 每次 Subscribe 后断言 sub.Priority 与 sub.Topic 都按预期落库。
// 为什么需要:订阅时把优先级记错,会让高优先级消息排到低优先级队列之后处理,表现为
// 限流/关服类消息迟迟不生效且无任何报错。保留「不传参数」这一格,是因为默认值最容易
// 在重构里被顺手改掉。
func TestSubscribe_Priority(t *testing.T) {
	cases := []struct {
		name     string
		topic    string
		priority []MessagePriority
		want     MessagePriority
	}{
		{"默认优先级", "topic_default", nil, TOPIC_PRIORITY_NORMAL},
		{"显式 CRITICAL", "topic_critical", []MessagePriority{TOPIC_PRIORITY_CRITICAL}, TOPIC_PRIORITY_CRITICAL},
		{"显式 HIGH", "topic_high", []MessagePriority{TOPIC_PRIORITY_HIGH}, TOPIC_PRIORITY_HIGH},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mq := NewMessageQueueApp()
			err := mq.Subscribe(context.Background(), tc.topic, func(ctx context.Context, msg string) error { return nil }, tc.priority...)
			if err != nil {
				t.Fatal(err)
			}
			sub, ok := mq.subs[tc.topic]
			if !ok {
				t.Fatalf("%s should be subscribed", tc.topic)
			}
			if sub.Priority != tc.want {
				t.Fatalf("priority = %v, want %v", sub.Priority, tc.want)
			}
			if sub.Topic != tc.topic {
				t.Fatalf("topic = %s, want %s", sub.Topic, tc.topic)
			}
		})
	}
}

func TestSubscribe_MultipleTopics(t *testing.T) {
	mq := NewMessageQueueApp()
	if err := mq.Subscribe(context.Background(), "t1", func(ctx context.Context, msg string) error { return nil }, TOPIC_PRIORITY_CRITICAL); err != nil {
		t.Fatal(err)
	}
	if err := mq.Subscribe(context.Background(), "t2", func(ctx context.Context, msg string) error { return nil }, TOPIC_PRIORITY_HIGH); err != nil {
		t.Fatal(err)
	}
	if err := mq.Subscribe(context.Background(), "t3", func(ctx context.Context, msg string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(mq.subs) != 3 {
		t.Fatalf("expected 3 subs, got %d", len(mq.subs))
	}
}

func TestSubscribe_Overwrite(t *testing.T) {
	mq := NewMessageQueueApp()
	handler1 := func(ctx context.Context, msg string) error { return nil }
	handler2 := func(ctx context.Context, msg string) error { return errors.New("v2") }
	if err := mq.Subscribe(context.Background(), "t1", handler1, TOPIC_PRIORITY_HIGH); err != nil {
		t.Fatal(err)
	}
	if err := mq.Subscribe(context.Background(), "t1", handler2, TOPIC_PRIORITY_CRITICAL); err != nil {
		t.Fatal(err)
	}
	sub := mq.subs["t1"]
	if sub.Priority != TOPIC_PRIORITY_CRITICAL {
		t.Fatalf("expected CRITICAL after overwrite, got %v", sub.Priority)
	}
	// Verify the handler was replaced
	err := sub.Handler(context.Background(), "test")
	if err == nil || err.Error() != "v2" {
		t.Fatal("handler should be overwritten")
	}
}

// ========== processMessages (with stop signal) ==========

// TestProcessMessages_DispatchesFromEveryChannel:processMessages 必须从**每一个**优先级
// 通道取消息并交给对应的 handler,不是只服务默认通道。
// 为什么需要:多路复用写错一处(下标错位、漏接某个 case)会让某个订阅的 topic 永远收不到
// 消息——静默的消息黑洞,发送方以为已投递。
// 注意本测试**不涉及优先级排序**:processMessages 用 reflect.Select,多个 case 就绪时
// 是伪随机选择,CRITICAL 并不优先于 NORMAL。当前无任何生产调用方传非默认优先级,
// 故不测排序;若将来启用,须先把分发改成显式优先级轮询再写该断言。
func TestProcessMessages_DispatchesFromEveryChannel(t *testing.T) {
	mq := NewMessageQueueApp()
	ctx := context.Background()

	received := make([]string, int(TOPIC_PRIORITY_MAX))
	var mu sync.Mutex
	for p := range received {
		priority := MessagePriority(p)
		topic := "topic_" + string(rune('a'+p))
		if err := mq.Subscribe(ctx, topic, func(ctx context.Context, msg string) error {
			mu.Lock()
			received[p] = msg
			mu.Unlock()
			return nil
		}, priority); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	wg.Go(func() { _ = mq.processMessages(ctx) })
	defer func() { close(mq.stopCh); wg.Wait() }()

	for p := range received {
		priority := MessagePriority(p)
		mq.priorityCh[priority] <- PriorityData{
			Topic:   "topic_" + string(rune('a'+p)),
			Data:    "msg",
			Handler: mq.subs["topic_"+string(rune('a'+p))].Handler,
		}
	}

	for range 100 {
		mu.Lock()
		done := true
		for _, r := range received {
			if r == "" {
				done = false
			}
		}
		mu.Unlock()
		if done {
			return
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	t.Fatalf("not all channels dispatched: %v", received)
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
	}, TOPIC_PRIORITY_NORMAL); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Go(func() { _ = mq.processMessages(ctx) })
	defer func() { close(mq.stopCh); wg.Wait() }()

	for _, msg := range []string{"first", "second"} {
		mq.priorityCh[TOPIC_PRIORITY_NORMAL] <- PriorityData{
			Topic: "err", Data: msg, Handler: mq.subs["err"].Handler,
		}
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
