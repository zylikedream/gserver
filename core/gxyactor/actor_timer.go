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

type ActorTimer struct {
	cronSchedulers map[string]*CronInstance
	timer          *gtimer.Timer
	cron           *gcron.Cron
	CronTm         time.Time `bson:"cron_tm"`
	funcs          map[string]callbackFunc
	pid            PID
}

func NewActorTimer(pid PID) *ActorTimer {
	return &ActorTimer{
		funcs:          map[string]callbackFunc{},
		cronSchedulers: map[string]*CronInstance{},
		timer:          gtimer.New(),
		cron:           gcron.New(),
		pid:            pid,
	}
}

func (s *ActorTimer) AddTick(ctx context.Context, tick *Tick, fun callbackFunc) {
	s.funcs[tick.Name] = fun
	s.timer.AddSingleton(ctx, tick.Tick, func(ctx context.Context) {
		ActorSystem().Send(s.pid, &ActorTimerMsg{
			Name: tick.Name,
			Time: time.Now().Unix(),
		})
	})
}

func (s *ActorTimer) Active(ctx context.Context, msg *ActorTimerMsg) {
	if fun, ok := s.funcs[msg.Name]; ok {
		fun(ctx, time.Unix(msg.Time, 0))
	}
}

func (s *ActorTimer) AddCron(ctx context.Context, cron *Cron, fun callbackFunc) {
	s.funcs[cron.Name] = fun
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
