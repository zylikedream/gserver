package gxyactor

import (
	"context"
	"testing"

	"gserver/protocol/pb"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
)

// notifyActor 是**不承载实体**的 actor,但它按消息类型分派。
//
// 分派属于"是个 actor"这一层:任何 actor 都可以只声明"哪类消息由哪个方法处理",
// 与是否参与所有权协议无关(见 ADR 0015)。
type notifyActor struct {
	*Actor
	handled []string
}

// HandleAck 的方法签名满足分派约定(接收者、context、单参数)。
func (a *notifyActor) HandleAck(_ context.Context, msg *pb.Ack) (any, error) {
	a.handled = append(a.handled, msg.GetReason())
	return nil, nil
}

func spawnNotify(t *testing.T) (*notifyActor, *unit.Subject) {
	t.Helper()
	var a *notifyActor
	subj, err := unit.Spawn(t, ActorFactory("notify", func() Business {
		a = &notifyActor{Actor: NewActor()}
		return a
	}), gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	return a, subj
}

// 不承载实体的 actor 同样按消息类型分派到业务方法。
func TestNonEntityActorDispatchesByMessageType(t *testing.T) {
	a, subj := spawnNotify(t)

	subj.SendMessage(gen.PID{}, &pb.Ack{Reason: "done"})

	if len(a.handled) != 1 {
		t.Fatalf("分派到 HandleAck 次数 = %d, want 1", len(a.handled))
	}
	if a.handled[0] != "done" {
		t.Fatalf("收到的内容 = %q, want done", a.handled[0])
	}
}

// 没有对应处理器的消息视为"本 actor 不处理这类消息",不终止进程。
//
// 每个 actor 都经内嵌基类获得默认入口,若把"没有这条路由"当致命,任何无关消息
// 都会杀掉 actor(例如激活协调者收到一条杂散异步消息)。
func TestUnroutedMessageDoesNotTerminateActor(t *testing.T) {
	a, subj := spawnNotify(t)

	subj.SendMessage(gen.PID{}, "no handler for this")

	if subj.Terminated() {
		t.Fatal("没有对应处理器不应导致终止")
	}
	if len(a.handled) != 0 {
		t.Fatalf("不应分派到任何方法,实际 %v", a.handled)
	}
}

// 显式调用 DispatchDefault 时,没有处理器仍是错误——调用方正是要判断
// "有没有这条路由"(如协议错误处理)。
func TestDispatchDefaultReportsMissingRoute(t *testing.T) {
	a, _ := spawnNotify(t)

	if _, err := a.DispatchDefault(&pb.Ack{Reason: "x"}); err != nil {
		t.Fatalf("已注册的处理器不应报错: %v", err)
	}
	if _, err := a.DispatchDefault("no handler for this"); err == nil {
		t.Fatal("没有对应处理器时 DispatchDefault 必须返回错误")
	}
}
