package gxymq

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func noopHandler(context.Context, string) error { return nil }

// TestConsume_ReturnsOnClosedChannel 消息通道关闭后 consume 必须返回。
// 为什么需要:go-redis 在连接池关闭时会 close 掉消息通道,而关闭后的通道 receive
// 立刻返回零值且永不阻塞 —— 把零值当成"空消息"跳过,循环就变成 100% CPU 忙等。
func TestConsume_ReturnsOnClosedChannel(t *testing.T) {
	mq := &RedisMQ{stopCh: make(chan struct{})}
	ch := make(chan *redis.Message)
	close(ch)

	done := make(chan struct{})
	go func() {
		mq.consume(context.Background(), "topic", ch, noopHandler)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("通道关闭后 consume 未返回:会忙等烧 CPU")
	}
}

// TestConsume_DeliversMessages 正常路径:收到的消息交给 handler。
func TestConsume_DeliversMessages(t *testing.T) {
	mq := &RedisMQ{stopCh: make(chan struct{})}
	ch := make(chan *redis.Message, 1)
	received := make(chan string, 1)

	go mq.consume(context.Background(), "topic", ch, func(_ context.Context, msg string) error {
		received <- msg
		return nil
	})
	ch <- &redis.Message{Payload: "hello"}

	select {
	case got := <-received:
		if got != "hello" {
			t.Fatalf("handler 收到 %q, want hello", got)
		}
	case <-time.After(time.Second):
		t.Fatal("handler 未收到消息")
	}
}

// TestConsume_ReturnsOnStop 停机信号必须能终止循环,不能只依赖通道关闭。
func TestConsume_ReturnsOnStop(t *testing.T) {
	mq := &RedisMQ{stopCh: make(chan struct{})}
	ch := make(chan *redis.Message) // 一直不投递:循环只能靠 stopCh 退出

	done := make(chan struct{})
	go func() {
		mq.consume(context.Background(), "topic", ch, noopHandler)
		close(done)
	}()

	close(mq.stopCh)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("停机后 consume 未返回")
	}
}
