package logic

import (
	"context"
	"testing"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxyactor/gxyactortest"

	"ergo.services/ergo/gen"
)

// 定时器触发经邮箱投递 ActorTimerMsg,由基类路由到已注册的回调。
// 会话把空闲检查挂在周期定时器上,若该消息不进入基类路由,超时清理会静默失效。
func TestSessionRoutesTimerMessage(t *testing.T) {
	ep := newFakeEndpoint(t)
	gxyactortest.StubOwnership(t)
	s, subj := gxyactortest.Spawn(t, func() *Session { return NewSession(ep) })

	fired := 0
	s.Timer().AddTick("probe", time.Hour, func(context.Context) { fired++ })

	subj.SendMessage(gen.PID{}, gxyactor.ActorTimerMsg{Name: "probe"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && fired == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if fired != 1 {
		t.Fatalf("定时消息未被路由到回调: fired=%d, want 1", fired)
	}
}
