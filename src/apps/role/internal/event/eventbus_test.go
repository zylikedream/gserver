package event

import (
	"context"
	"testing"
)

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
