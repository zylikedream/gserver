package gxyactor

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/os/gcron"
	"github.com/gogf/gf/v2/os/gtimer"
	"github.com/robfig/cron/v3"
)

type Ticker interface {
}

type callbackFunc func(context.Context, time.Time)

type CronInstance struct {
	cron *Cron
	fun  callbackFunc
}

type TimerInfo struct {
	callbackFunc callbackFunc
	name         string
	entry        *gtimer.Entry
}

type ActorTimer struct {
	cronSchedulers map[string]*CronInstance
	timer          *gtimer.Timer
	cron           *gcron.Cron
	CronTm         time.Time `bson:"cron_tm"`
	timerInfos     map[string]TimerInfo
	pid            PID
}

func NewActorTimer(pid PID) *ActorTimer {
	return &ActorTimer{
		timerInfos:     map[string]TimerInfo{},
		cronSchedulers: map[string]*CronInstance{},
		timer:          gtimer.New(),
		cron:           gcron.New(),
		pid:            pid,
	}
}

func (s *ActorTimer) AddTick(ctx context.Context, tick *Tick, fun callbackFunc) {
	entry := s.timer.AddSingleton(ctx, tick.Tick, func(ctx context.Context) {
		ActorSystem().Send(s.pid, &ActorTimerMsg{
			Name: tick.Name,
			Time: time.Now().Unix(),
		})
	})
	s.timerInfos[tick.Name] = TimerInfo{
		callbackFunc: fun,
		name:         tick.Name,
		entry:        entry,
	}
}

func (s *ActorTimer) AddOnce(ctx context.Context, once *Once, fun callbackFunc) {
	entry := s.timer.AddOnce(ctx, once.EndTime, func(ctx context.Context) {
		ActorSystem().Send(s.pid, &ActorTimerMsg{
			Name: once.Name,
			Time: time.Now().Unix(),
		})
	})
	s.timerInfos[once.Name] = TimerInfo{
		callbackFunc: fun,
		name:         once.Name,
		entry:        entry,
	}
}

func (s *ActorTimer) Active(ctx context.Context, msg *ActorTimerMsg) {
	if info, ok := s.timerInfos[msg.Name]; ok {
		info.callbackFunc(ctx, time.Unix(msg.Time, 0))
	}
}

func (s *ActorTimer) AddCron(ctx context.Context, cron *Cron, fun callbackFunc) {
	s.cron.AddSingleton(ctx, cron.Pattern, func(ctx context.Context) {
		s.SetCronTm(time.Now())
		ActorSystem().Send(s.pid, &ActorTimerMsg{
			Name: cron.Name,
			Time: s.CronTm.Unix(),
		})
	})
	s.cronSchedulers[cron.Name] = &CronInstance{cron: cron, fun: fun}
}

func (s *ActorTimer) RestoreCron(ctx context.Context) {
	p := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.DowOptional)
	tm := s.GetCronTm()
	for _, c := range s.cronSchedulers {
		sched, err := p.Parse(c.cron.Pattern)
		if err != nil {
			continue
		}
		nextTm := sched.Next(tm)
		if time.Now().After(nextTm) {
			c.fun(ctx, time.Now())
		}
	}
}

func (s *ActorTimer) GetCronTm() time.Time {
	return s.CronTm
}

func (s *ActorTimer) SetCronTm(tm time.Time) {
	s.CronTm = tm
}

func (s *ActorTimer) Cancel(name string) {
	if info, ok := s.timerInfos[name]; ok {
		info.entry.Stop()
		delete(s.timerInfos, name)
	}
}
