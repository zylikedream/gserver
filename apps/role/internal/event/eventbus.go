package event

import "github.com/gogf/gf/v2/util/guid"

type EventRef string
type EventType string
type EventSubType string

type EventParam struct {
	EType EventType
	Data  any
}

type eventHandler struct {
	ref     EventRef
	handler func(event EventParam)
}

type IEventBus interface {
	Subscribe(eventType EventType, handler func(event EventParam)) EventRef
	Unsubscribe(eventType EventType, ref EventRef)
	Publish(eventType EventType, data any)
}

type EventBus struct {
	eventHandlers map[EventType][]eventHandler
}

func NewEventBus() *EventBus {
	guid.S()
	return &EventBus{
		eventHandlers: make(map[EventType][]eventHandler),
	}
}

func (e *EventBus) Subscribe(eventType EventType, handler func(event EventParam)) EventRef {
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

func (e *EventBus) Publish(eventType EventType, data any) {
	handlers := e.eventHandlers[eventType]
	for _, h := range handlers {
		h.handler(EventParam{
			EType: eventType,
			Data:  data,
		})
	}
}
