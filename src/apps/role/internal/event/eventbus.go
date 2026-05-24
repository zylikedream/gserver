package event

import (
	"context"

	"github.com/gogf/gf/v2/util/guid"
)

type EventRef string
type EventType string

type EventParam struct {
	EType EventType
	Data  any
}

type queuedEvent struct {
	eventType EventType
	data      any
}

type eventHandler struct {
	ref     EventRef
	handler func(ctx context.Context, event EventParam)
}

type IEventBus interface {
	Subscribe(eventType EventType, handler func(ctx context.Context, event EventParam)) EventRef
	Unsubscribe(eventType EventType, ref EventRef)
	Publish(ctx context.Context, eventType EventType, data any)
}

type EventBus struct {
	eventHandlers map[EventType][]eventHandler
	eventQueue    []queuedEvent
	publishing    bool
}

func NewEventBus() *EventBus {
	return &EventBus{
		eventHandlers: make(map[EventType][]eventHandler),
	}
}

func (e *EventBus) Subscribe(eventType EventType, handler func(ctx context.Context, event EventParam)) EventRef {
	ref := EventRef(guid.S())
	e.eventHandlers[eventType] = append(e.eventHandlers[eventType], eventHandler{
		ref:     ref,
		handler: handler,
	})
	return ref
}

func (e *EventBus) Unsubscribe(eventType EventType, ref EventRef) {
	handlers := e.eventHandlers[eventType]
	for i, h := range handlers {
		if h.ref == ref {
			e.eventHandlers[eventType] = append(handlers[:i], handlers[i+1:]...)
			break
		}
	}
}

func (e *EventBus) Publish(ctx context.Context, eventType EventType, data any) {
	e.eventQueue = append(e.eventQueue, queuedEvent{eventType: eventType, data: data})
	if e.publishing {
		return
	}
	e.publishing = true
	defer func() {
		e.publishing = false
	}()
	for len(e.eventQueue) > 0 {
		ev := e.eventQueue[0]
		e.eventQueue = e.eventQueue[1:]
		handlers := append([]eventHandler(nil), e.eventHandlers[ev.eventType]...)
		for _, h := range handlers {
			h.handler(ctx, EventParam{
				EType: ev.eventType,
				Data:  ev.data,
			})
		}
	}
}
