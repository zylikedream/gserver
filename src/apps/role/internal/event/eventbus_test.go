package event

import (
	"context"
	"testing"
)

// TestEventBusPublishNestedEventsAfterCurrentHandlers:处理函数里重入 Publish 的事件必须排队到当前事件的所有兄弟 handler 跑完之后。
// 为什么需要:Publish 用 publishing 标志 + 队列实现重入保护。若去掉标志让嵌套事件立即执行,
// 迭代顺序被重入改写,嵌套事件会在兄弟 handler 之前触发——跨事件的因果被破坏,且事件可能被发布两次或一次都不发。
func TestEventBusPublishNestedEventsAfterCurrentHandlers(t *testing.T) {
	bus := NewEventBus()
	var order []string
	ctx := context.Background()
	bus.Subscribe(EVENT_BREED_START, func(ctx context.Context, event EventParam) {
		order = append(order, "a1")
		bus.Publish(ctx, EVENT_BREED_FINISH, nil)
		order = append(order, "a1_done")
	})
	bus.Subscribe(EVENT_BREED_START, func(ctx context.Context, event EventParam) {
		order = append(order, "a2")
	})
	bus.Subscribe(EVENT_BREED_FINISH, func(ctx context.Context, event EventParam) {
		order = append(order, "b1")
	})

	bus.Publish(ctx, EVENT_BREED_START, nil)

	want := []string{"a1", "a1_done", "a2", "b1"}
	if len(order) != len(want) {
		t.Fatalf("expected %v, got %v", want, order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, order)
		}
	}
}

// TestEventBusNestedEventsAreFIFO:一个 handler 里连续 Publish 两个事件,必须按入队顺序逐个派发(a,b,c)。
// 为什么需要:队列是 FIFO,这是事件顺序唯一的契约。若改成栈或对嵌套事件就地分派,
// 依赖先发布的 handler 会后执行,培养/收获等连锁状态推进顺序颠倒,事件携带的数据就来自错误的中间状态。
func TestEventBusNestedEventsAreFIFO(t *testing.T) {
	bus := NewEventBus()
	var order []string
	ctx := context.Background()

	bus.Subscribe(EVENT_BREED_START, func(ctx context.Context, event EventParam) {
		order = append(order, "a")
		bus.Publish(ctx, EVENT_BREED_FINISH, nil)
		bus.Publish(ctx, EVENT_PLANT_FLOWER, nil)
	})
	bus.Subscribe(EVENT_BREED_FINISH, func(ctx context.Context, event EventParam) {
		order = append(order, "b")
	})
	bus.Subscribe(EVENT_PLANT_FLOWER, func(ctx context.Context, event EventParam) {
		order = append(order, "c")
	})

	bus.Publish(ctx, EVENT_BREED_START, nil)

	want := []string{"a", "b", "c"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, order)
		}
	}
}
