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

func TestProcessMessages_DispatchByPriority(t *testing.T) {
	mq := NewMessageQueueApp()
	ctx := context.Background()

	var mu sync.Mutex
	var received []string

	if err := mq.Subscribe(ctx, "normal", func(ctx context.Context, msg string) error {
		mu.Lock()
		received = append(received, msg)
		mu.Unlock()
		return nil
	}, TOPIC_PRIORITY_NORMAL); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		_ = mq.processMessages(ctx)
	})

	time.Sleep(10 * time.Millisecond)

	// Send a message via the priority channel
	mq.priorityCh[TOPIC_PRIORITY_NORMAL] <- PriorityData{
		Topic:   "normal",
		Data:    "hello",
		Handler: mq.subs["normal"].Handler,
	}

	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if len(received) != 1 || received[0] != "hello" {
		t.Fatalf("expected [hello], got %v", received)
	}
	mu.Unlock()

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
