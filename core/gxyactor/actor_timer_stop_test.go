package gxyactor_test

import (
	"context"
	"testing"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxyactor/gxyactortest"

	"ergo.services/ergo/gen"
)

// stopOnTickActor 在定时回调里请求停止,用于验证 Stop 的"回调返回后终止"契约。
type stopOnTickActor struct {
	gxyactor.BaseActor
	terminated chan struct{}
}

func newStopOnTickActor() *stopOnTickActor {
	a := &stopOnTickActor{terminated: make(chan struct{})}
	a.BaseActor = *gxyactor.NewBaseActor()
	return a
}

func (a *stopOnTickActor) Terminate(ctx context.Context, err error) {
	select {
	case <-a.terminated:
	default:
		close(a.terminated)
	}
}

// TestTimerCallback_StopTerminatesActor: 在定时回调里调用 Stop,actor 必须在
// 这条定时消息处理完后终止(BaseActor.Stop 的契约)。
//
// 改前 timer/cron 分支固定返回 nil,Stop 只在后续普通消息路径才可能生效;
// 于是 role 的 single_alive(登出 10 分钟后回收)与落盘失败后的停止都永远不生效。
func TestTimerCallback_StopTerminatesActor(t *testing.T) {
	a, subj := gxyactortest.Spawn(t, "timer_stop", newStopOnTickActor)

	a.Timer().AddTick("stop", time.Hour, func(context.Context) { a.Stop(nil) })
	subj.SendMessage(gen.PID{}, gxyactor.ActorTimerMsg{Name: "stop"})

	select {
	case <-a.terminated:
	case <-time.After(2 * time.Second):
		t.Fatal("actor did not terminate after Stop in timer callback")
	}
}
