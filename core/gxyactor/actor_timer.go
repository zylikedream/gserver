package gxyactor

import (
	"context"
	"gserver/core/gxytimer"
	"time"

	"github.com/gogf/gf/v2/util/gutil"
)

type ICronState interface {
	GetCronTm() time.Time
	SetCronTm(tm time.Time)
}

type ActorTimerCallback func(ActorContext, gxytimer.TimerActiveInfo)

type ActorTimer struct {
	*gxytimer.GxyTimer
	cronState     ICronState
	callbackFuncs map[string]ActorTimerCallback
	cronNames     map[string]struct{}
	pid           PID
}

func NewActorTimer(pid PID) *ActorTimer {
	return &ActorTimer{
		GxyTimer:      gxytimer.NewTimer(),
		callbackFuncs: make(map[string]ActorTimerCallback),
		cronNames:     make(map[string]struct{}),
		pid:           pid,
	}
}

func (s *ActorTimer) SetCronState(cronState ICronState) {
	s.cronState = cronState
}

func (s *ActorTimer) AddTick(ctx context.Context, tick *gxytimer.Tick, fun ActorTimerCallback) {
	s.GxyTimer.AddTick(ctx, tick, func(ctx context.Context, info gxytimer.TimerActiveInfo) {
		_ = LocalSend(ctx, s.pid, ActorTimerMsg(info))
	})
	s.callbackFuncs[tick.Name] = fun
}

func (s *ActorTimer) AddOnce(ctx context.Context, once *gxytimer.Once, fun ActorTimerCallback) {
	s.GxyTimer.AddOnce(ctx, once, func(ctx context.Context, info gxytimer.TimerActiveInfo) {
		_ = LocalSend(ctx, s.pid, ActorTimerMsg(info))
	})
	s.callbackFuncs[once.Name] = fun
}

func (s *ActorTimer) Active(ctx ActorContext, msg ActorTimerMsg) error {
	if _, ok := s.cronNames[msg.Name]; ok && s.cronState != nil {
		// Cron state is actor state. Update it only after the timer event has
		// entered and is being handled by the actor mailbox.
		s.cronState.SetCronTm(msg.Time)
	}
	if fun, ok := s.callbackFuncs[msg.Name]; ok {
		return gutil.Try(ctx, func(context.Context) {
			fun(ctx, gxytimer.TimerActiveInfo(msg))
		})
	}
	return nil
}

func (s *ActorTimer) AddCron(ctx context.Context, cron *gxytimer.Cron, fun ActorTimerCallback) {
	_ = s.GxyTimer.AddCron(ctx, cron, func(ctx context.Context, info gxytimer.TimerActiveInfo) {
		_ = LocalSend(ctx, s.pid, ActorTimerMsg(info))
	})
	s.callbackFuncs[cron.Name] = fun
	s.cronNames[cron.Name] = struct{}{}
}

func (s *ActorTimer) RestoreCron(ctx context.Context) {
	if s.cronState != nil {
		s.GxyTimer.RestoreCron(ctx, s.cronState.GetCronTm())
	}
}
