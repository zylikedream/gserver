package gxytimer

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/os/gcron"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/os/gtimer"
	"github.com/robfig/cron/v3"
)

type TimerActiveInfo struct {
	Name          string        // 定时器的名称
	Time          time.Time     // 定时器触发的时间
	Duration      time.Duration // 定时器的间隔时间
	IntervalCount int           // 比如timer间隔时间是5s,如果duraction是63s, 那么就会触发12次
}

type CallbackFunc func(ctx context.Context, info TimerActiveInfo)

type timerInfo struct {
	name       string
	cron       *Cron
	cronEntry  *gcron.Entry
	tick       *Tick
	Once       *Once
	timerEntry *gtimer.Entry
	fun        CallbackFunc
	duration   time.Duration
}

func getCronDuration(pattern string) time.Duration {
	p := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.DowOptional)
	sched, err := p.Parse(pattern)
	if err != nil {
		return 0
	}
	now := time.Now()
	next1 := sched.Next(now)
	next2 := sched.Next(next1)
	// 计算间隔
	interval := next2.Sub(next1)
	return interval
}

func newCronTimerInfo(cron *Cron, fun CallbackFunc) *timerInfo {
	return &timerInfo{
		name:     cron.Name,
		cron:     cron,
		fun:      fun,
		duration: getCronDuration(cron.Pattern),
	}
}

func newTickTimerInfo(tick *Tick, fun CallbackFunc) *timerInfo {
	return &timerInfo{
		name:     tick.Name,
		tick:     tick,
		fun:      fun,
		duration: tick.Interval,
	}
}

func newOnceTimerInfo(once *Once, fun CallbackFunc) *timerInfo {
	return &timerInfo{
		name:     once.Name,
		Once:     once,
		fun:      fun,
		duration: once.After,
	}
}

type GxyTimer struct {
	timer      *gtimer.Timer
	cron       *gcron.Cron
	CronTm     time.Time `bson:"cron_tm"`
	timerInfos map[string]*timerInfo
}

func NewTimer() *GxyTimer {
	return &GxyTimer{
		timerInfos: map[string]*timerInfo{},
		timer:      gtimer.New(),
		cron:       gcron.New(),
	}
}

func (s *GxyTimer) AddTick(ctx context.Context, tick *Tick, fun CallbackFunc) {
	entry := s.timer.AddSingleton(ctx, tick.Interval, func(ctx context.Context) {
		now := time.Now()
		fun(ctx, TimerActiveInfo{
			Name:          tick.Name,
			Time:          now,
			Duration:      tick.Interval,
			IntervalCount: 1,
		})
	})
	timerInfo := newTickTimerInfo(tick, fun)
	timerInfo.timerEntry = entry
	s.timerInfos[tick.Name] = timerInfo
}

func (s *GxyTimer) AddOnce(ctx context.Context, once *Once, fun CallbackFunc) {
	entry := s.timer.AddOnce(ctx, once.After, func(ctx context.Context) {
		now := time.Now()
		fun(ctx, TimerActiveInfo{
			Name:          once.Name,
			Time:          now,
			Duration:      once.After,
			IntervalCount: 1,
		})
	})
	timerInfo := newOnceTimerInfo(once, fun)
	timerInfo.timerEntry = entry
	s.timerInfos[once.Name] = timerInfo
}

func (s *GxyTimer) AddCron(ctx context.Context, cron *Cron, fun CallbackFunc) error {
	timerInfo := newCronTimerInfo(cron, fun)
	entry, err := s.cron.AddSingleton(ctx, cron.Pattern, func(ctx context.Context) {
		now := time.Now()
		fun(ctx, TimerActiveInfo{
			Name:          cron.Name,
			Time:          now,
			Duration:      timerInfo.duration,
			IntervalCount: 1,
		})
	})
	if err != nil {
		return err
	}
	timerInfo.cronEntry = entry
	s.timerInfos[cron.Name] = timerInfo
	return nil
}

func (s *GxyTimer) RestoreCron(ctx context.Context, tm time.Time) {
	p := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.DowOptional)
	for _, info := range s.timerInfos {
		if info.cron == nil {
			continue
		}
		sched, err := p.Parse(info.cron.Pattern)
		if err != nil {
			continue
		}
		nextTm := sched.Next(tm)
		now := time.Now()
		if now.After(nextTm) {
			duration := now.Sub(nextTm)
			intervalCount := s.getIntervalCount(info.duration, duration)
			info.fun(ctx, TimerActiveInfo{
				Name:          info.name,
				Time:          now,
				Duration:      info.duration,
				IntervalCount: intervalCount,
			})
		}
	}
}

func (s *GxyTimer) Cancel(ctx context.Context, name string) {
	if info, ok := s.timerInfos[name]; ok {
		glog.Debugf(ctx, "Cancel timer %s", name)
		if info.timerEntry != nil {
			info.timerEntry.Close()
		}
		if info.cronEntry != nil {
			info.cronEntry.Close()
		}
		delete(s.timerInfos, name)
	}
}

func (s *GxyTimer) CancelAll(ctx context.Context) {
	for name := range s.timerInfos {
		s.Cancel(ctx, name)
	}
	// s.timer.Close()
	// s.cron.Close()
}

func (s *GxyTimer) getIntervalCount(interval time.Duration, duration time.Duration) int {
	return int(duration / interval)
}
