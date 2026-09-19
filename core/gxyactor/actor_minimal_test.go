package gxyactor

import (
	"testing"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
)

// plainActor 是"简单 actor"的写法示例:不承载实体、不用反射分派。
//
// 它只内嵌 Actor,并实现业务入口 Receive —— 没有分派目标、没有注册步骤、
// 构造时不传自身。门面把已还原的消息直接交给它。
type plainActor struct {
	*Actor
	handled []any
}

func newPlainActor() *plainActor {
	a := &plainActor{}
	a.Actor = NewActor("plain")
	return a
}

func (a *plainActor) Init(args ...any) error {
	return a.Actor.Init(args...)
}

func (a *plainActor) Receive(_ gen.PID, msg any) (any, error) {
	a.handled = append(a.handled, msg)
	return nil, nil
}

// 简单 actor 不必经过任何分派机制:实现 Receive 即可收到消息。
func TestPlainActorReceivesMessage(t *testing.T) {
	a := newPlainActor()
	subj, err := unit.Spawn(t, func() gen.ProcessBehavior { return a }, gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	const payload = "hello"
	subj.SendMessage(gen.PID{}, payload)

	if len(a.handled) != 1 {
		t.Fatalf("Receive 调用次数 = %d, want 1", len(a.handled))
	}
	if a.handled[0] != payload {
		t.Fatalf("收到的消息 = %#v, want %q", a.handled[0], payload)
	}
}

// returnActor 演示同步请求入口:返回值经响应通道回给调用方。
type returnActor struct {
	*Actor
}

func (a *returnActor) ReceiveCall(_ gen.PID, _ gen.Ref, _ any) (any, error) {
	return "called", nil
}

// 未实现 Receive 的 actor 不处理异步消息,门面静默跳过而非报错。
func TestActorWithoutEntryIgnoresMessage(t *testing.T) {
	a := &returnActor{Actor: NewActor("return_only")}
	subj, err := unit.Spawn(t, func() gen.ProcessBehavior { return a }, gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	// 不 panic、不终止即可。
	subj.SendMessage(gen.PID{}, "ignored")
	if subj.Terminated() {
		t.Fatal("未实现业务入口不应导致终止")
	}
}
