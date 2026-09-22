package gxyactor

import (
	"testing"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
)

// plainActor 是"简单 actor"的写法示例:只内嵌 Actor、只实现 HandleMessage。
// 不涉及分派、不承载实体、构造时不传自身。
type plainActor struct {
	*Actor
	handled []any
}

func (a *plainActor) HandleMessage(msg any) (any, error) {
	a.handled = append(a.handled, msg)
	return nil, nil
}

func spawnPlain(t *testing.T) (*plainActor, *unit.Subject) {
	t.Helper()
	var p *plainActor
	subj, err := unit.Spawn(t, ActorFactory("plain", func() Business {
		p = &plainActor{Actor: NewActor()}
		return p
	}), gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	return p, subj
}

// 简单 actor 不必经过任何分派机制:实现 HandleMessage 即可收到消息。
func TestPlainActorReceivesMessage(t *testing.T) {
	a, subj := spawnPlain(t)

	const payload = "hello"
	subj.SendMessage(gen.PID{}, payload)

	if len(a.handled) != 1 {
		t.Fatalf("HandleMessage 调用次数 = %d, want 1", len(a.handled))
	}
	if a.handled[0] != payload {
		t.Fatalf("收到的消息 = %#v, want %q", a.handled[0], payload)
	}
}

// returnActor 只实现同步请求入口。
type returnActor struct {
	*Actor
}

func (a *returnActor) HandleCall(_ any) (any, error) { return "called", nil }

// 未实现 HandleMessage 的 actor 不处理异步消息,门面静默跳过而非报错。
func TestActorWithoutEntryIgnoresMessage(t *testing.T) {
	var a *returnActor
	subj, err := unit.Spawn(t, ActorFactory("return_only", func() Business {
		a = &returnActor{Actor: NewActor()}
		return a
	}), gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// 不 panic、不终止即可。
	subj.SendMessage(gen.PID{}, "ignored")
	if subj.Terminated() {
		t.Fatal("未实现业务入口不应导致终止")
	}
}
