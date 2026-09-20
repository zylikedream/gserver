package chat

import (
	"context"
	"testing"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxyactor/gxyactortest"

	"ergo.services/ergo/gen"
)

// 定时器触发经邮箱投递 ActorTimerMsg,由基类路由到已注册的回调。
// 覆写 HandleMessage 的 actor 若不链式调用基类,定时器会静默失效——
// 而频道依赖周期存盘与空置自动停止。
func TestChannelActorRoutesTimerMessage(t *testing.T) {
	gxyactortest.StubOwnership(t)
	a, subj := gxyactortest.Spawn(t, NewChannelActor, "1_100")
	if a.channel == nil {
		a.channel = timerProbeChannel{}
		a.buffer = newRingBuffer(8)
	}

	fired := 0
	a.Timer().AddTick("probe", time.Hour, func(context.Context) { fired++ })

	subj.SendMessage(gen.PID{}, gxyactor.ActorTimerMsg{Name: "probe"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && fired == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if fired != 1 {
		t.Fatalf("定时消息未被路由到回调: fired=%d, want 1", fired)
	}
}

// timerProbeChannel 满足 IChannel,仅用于让实例可用。
type timerProbeChannel struct{}

func (timerProbeChannel) ChannelType() string          { return "probe" }
func (timerProbeChannel) RingBufferSize() int          { return 8 }
func (timerProbeChannel) SaveInterval() time.Duration  { return time.Hour }
func (timerProbeChannel) TableName() string            { return "probe" }
func (timerProbeChannel) CanWrite(int64, string) error { return nil }
func (timerProbeChannel) CanJoin(int64) bool           { return true }
