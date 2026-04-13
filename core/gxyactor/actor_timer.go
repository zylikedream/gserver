package gxyactor

import (
	"context"
	"gserver/core/gxytimer"
	"time"
)

type ICronState interface {
	GetCronTm() time.Time
	SetCronTm(tm time.Time)
}

type ActorTimer struct {
	*gxytimer.GxyTimer
	cronState     ICronState
	callbackFuncs map[string]gxytimer.CallbackFunc
	pid           PID
}

func NewActorTimer(pid PID) *ActorTimer {
	return &ActorTimer{
		GxyTimer:      gxytimer.NewTimer(),
		callbackFuncs: make(map[string]gxytimer.CallbackFunc),
		pid:           pid,
	}
}

func (s *ActorTimer) SetCronState(cronState ICronState) {
	s.cronState = cronState
}

func (s *ActorTimer) AddTick(ctx context.Context, tick *gxytimer.Tick, fun gxytimer.CallbackFunc) {
	s.GxyTimer.AddTick(ctx, tick, func(ctx context.Context, info gxytimer.TimerActiveInfo) {
		LocalSend(s.pid, ActorTimerMsg(info))
	})
	s.callbackFuncs[tick.Name] = fun
}

func (s *ActorTimer) AddOnce(ctx context.Context, once *gxytimer.Once, fun gxytimer.CallbackFunc) {
	s.GxyTimer.AddOnce(ctx, once, func(ctx context.Context, info gxytimer.TimerActiveInfo) {
		LocalSend(s.pid, ActorTimerMsg(info))
	})
	s.callbackFuncs[once.Name] = fun
}

func (s *ActorTimer) Active(ctx context.Context, msg ActorTimerMsg) {
	if fun, ok := s.callbackFuncs[msg.Name]; ok {
		fun(ctx, gxytimer.TimerActiveInfo(msg))
	}
}

func (s *ActorTimer) AddCron(ctx context.Context, cron *gxytimer.Cron, fun gxytimer.CallbackFunc) {
	s.GxyTimer.AddCron(ctx, cron, func(ctx context.Context, info gxytimer.TimerActiveInfo) {
		if s.cronState != nil {
			s.cronState.SetCronTm(info.Time)
		}
		LocalSend(s.pid, ActorTimerMsg(info))
	})
	s.callbackFuncs[cron.Name] = fun
}

func (s *ActorTimer) RestoreCron(ctx context.Context) {
	if s.cronState != nil {
		s.GxyTimer.RestoreCron(ctx, s.cronState.GetCronTm())
	}
}
