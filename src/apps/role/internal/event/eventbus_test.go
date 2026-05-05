package event

import "testing"

func TestEventBusPublishNestedEventsAfterCurrentHandlers(t *testing.T) {
	bus := NewEventBus()
	var order []string

	bus.Subscribe(EVENT_BREED_START, func(event EventParam) {
		order = append(order, "a1")
		bus.Publish(EVENT_BREED_FINISH, nil)
		order = append(order, "a1_done")
	})
	bus.Subscribe(EVENT_BREED_START, func(event EventParam) {
		order = append(order, "a2")
	})
	bus.Subscribe(EVENT_BREED_FINISH, func(event EventParam) {
		order = append(order, "b1")
	})

	bus.Publish(EVENT_BREED_START, nil)

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

	bus.Subscribe(EVENT_BREED_START, func(event EventParam) {
		order = append(order, "a")
		bus.Publish(EVENT_BREED_FINISH, nil)
		bus.Publish(EVENT_PLANT_FLOWER, nil)
	})
	bus.Subscribe(EVENT_BREED_FINISH, func(event EventParam) {
		order = append(order, "b")
	})
	bus.Subscribe(EVENT_PLANT_FLOWER, func(event EventParam) {
		order = append(order, "c")
	})

	bus.Publish(EVENT_BREED_START, nil)

	want := []string{"a", "b", "c"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, order)
		}
	}
}
