package gxymq

import (
	"context"
)

type MessagePriority int

const (
	TOPIC_PRIORITY_CRITICAL MessagePriority = iota
	TOPIC_PRIORITY_HIGH
	TOPIC_PRIORITY_NORMAL
	TOPIC_PRIORITY_MAX
)

type TopicHandler func(ctx context.Context, msg string) error

type SubInfo struct {
	Topic    string
	Priority MessagePriority
	Handler  TopicHandler
}

type PriorityData struct {
	Topic   string
	Data    string
	Handler TopicHandler
}
