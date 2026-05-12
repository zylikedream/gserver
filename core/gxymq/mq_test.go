package gxymq

import (
	"context"
	"errors"
	"reflect"
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

func TestNewMessageQueueApp_PriorityChannelBuffer(t *testing.T) {
	mq := NewMessageQueueApp()
	for i, ch := range mq.priorityCh {
		if cap(ch) != 1000 {
			t.Fatalf("priorityCh[%d] cap: expected 1000, got %d", i, cap(ch))
		}
	}
}

func TestNewMessageQueueApp_PriorityOrder(t *testing.T) {
	// Verify ordering: CRITICAL(0) < HIGH(1) < NORMAL(2)
	if TOPIC_PRIORITY_CRITICAL >= TOPIC_PRIORITY_HIGH {
		t.Fatal("CRITICAL should have highest priority (lowest value)")
	}
	if TOPIC_PRIORITY_HIGH >= TOPIC_PRIORITY_NORMAL {
		t.Fatal("HIGH should have higher priority than NORMAL")
	}
}

// ========== Subscribe ==========

func TestSubscribe_DefaultPriority(t *testing.T) {
	mq := NewMessageQueueApp()
	handler := func(ctx context.Context, msg string) error { return nil }
	err := mq.Subscribe(context.Background(), "topic1", handler)
	if err != nil {
		t.Fatal(err)
	}
	sub, ok := mq.subs["topic1"]
	if !ok {
		t.Fatal("topic1 should be subscribed")
	}
	if sub.Priority != TOPIC_PRIORITY_NORMAL {
		t.Fatalf("expected NORMAL priority, got %v", sub.Priority)
	}
	if sub.Topic != "topic1" {
		t.Fatalf("expected topic1, got %s", sub.Topic)
	}
}

func TestSubscribe_ExplicitPriority(t *testing.T) {
	mq := NewMessageQueueApp()
	handler := func(ctx context.Context, msg string) error { return nil }
	err := mq.Subscribe(context.Background(), "critical_topic", handler, TOPIC_PRIORITY_CRITICAL)
	if err != nil {
		t.Fatal(err)
	}
	sub, ok := mq.subs["critical_topic"]
	if !ok {
		t.Fatal("should be subscribed")
	}
	if sub.Priority != TOPIC_PRIORITY_CRITICAL {
		t.Fatalf("expected CRITICAL priority, got %v", sub.Priority)
	}
}

func TestSubscribe_HighPriority(t *testing.T) {
	mq := NewMessageQueueApp()
	handler := func(ctx context.Context, msg string) error { return nil }
	err := mq.Subscribe(context.Background(), "high_topic", handler, TOPIC_PRIORITY_HIGH)
	if err != nil {
		t.Fatal(err)
	}
	sub, ok := mq.subs["high_topic"]
	if !ok {
		t.Fatal("should be subscribed")
	}
	if sub.Priority != TOPIC_PRIORITY_HIGH {
		t.Fatalf("expected HIGH priority, got %v", sub.Priority)
	}
}

func TestSubscribe_MultipleTopics(t *testing.T) {
	mq := NewMessageQueueApp()
	mq.Subscribe(context.Background(), "t1", func(ctx context.Context, msg string) error { return nil }, TOPIC_PRIORITY_CRITICAL)
	mq.Subscribe(context.Background(), "t2", func(ctx context.Context, msg string) error { return nil }, TOPIC_PRIORITY_HIGH)
	mq.Subscribe(context.Background(), "t3", func(ctx context.Context, msg string) error { return nil })
	if len(mq.subs) != 3 {
		t.Fatalf("expected 3 subs, got %d", len(mq.subs))
	}
}

func TestSubscribe_Overwrite(t *testing.T) {
	mq := NewMessageQueueApp()
	handler1 := func(ctx context.Context, msg string) error { return nil }
	handler2 := func(ctx context.Context, msg string) error { return errors.New("v2") }
	mq.Subscribe(context.Background(), "t1", handler1, TOPIC_PRIORITY_HIGH)
	mq.Subscribe(context.Background(), "t1", handler2, TOPIC_PRIORITY_CRITICAL)
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
	wg.Add(1)
	go func() {
		defer wg.Done()
		mq.processMessages(ctx)
	}()
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

	mq.Subscribe(ctx, "normal", func(ctx context.Context, msg string) error {
		mu.Lock()
		received = append(received, msg)
		mu.Unlock()
		return nil
	}, TOPIC_PRIORITY_NORMAL)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mq.processMessages(ctx)
	}()

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

// ========== reflect.Select case count ==========

func TestProcessMessages_SelectCaseCount(t *testing.T) {
	mq := NewMessageQueueApp()
	// Build select cases the same way processMessages does
	cases := make([]reflect.SelectCase, len(mq.priorityCh)+1)
	cases[0] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(mq.stopCh)}
	for i, ch := range mq.priorityCh {
		cases[i+1] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)}
	}
	if len(cases) != int(TOPIC_PRIORITY_MAX)+1 {
		t.Fatalf("expected %d cases, got %d", TOPIC_PRIORITY_MAX+1, len(cases))
	}
}

// ========== MessageQueue singleton ==========

func TestMessageQueue_Singleton(t *testing.T) {
	mq1 := MessageQueue()
	mq2 := MessageQueue()
	if mq1 != mq2 {
		t.Fatal("MessageQueue() should return the same instance")
	}
}
