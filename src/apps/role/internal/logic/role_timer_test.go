package logic

import (
	"context"
	"gserver/src/lib"
	"testing"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxyactor/gxyactortest"

	"ergo.services/ergo/gen"
)

// 对照组:role 的 HandleMessage 链式调用了基类,定时消息应被正常路由。
// 与 chat / session 的同类测试对照,可确认"fired=0"是覆写回调未链式调用基类
// 所致,而不是测试方法本身有问题。
func TestRoleMainRoutesTimerMessage(t *testing.T) {
	orig := lookupAccountIDByRoleID
	t.Cleanup(func() { lookupAccountIDByRoleID = orig })
	lookupAccountIDByRoleID = func(context.Context, int64) (string, error) {
		return "acc_123", nil
	}

	gxyactortest.StubOwnership(t)
	r, subj := gxyactortest.Spawn(t, lib.ROLE_ACTOR_TYPE, NewRoleMain, int64(10001))

	fired := 0
	r.Timer().AddTick("probe", time.Hour, func(context.Context) { fired++ })

	subj.SendMessage(gen.PID{}, gxyactor.ActorTimerMsg{Name: "probe"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && fired == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if fired != 1 {
		t.Fatalf("定时消息未被路由到回调: fired=%d, want 1", fired)
	}
}
