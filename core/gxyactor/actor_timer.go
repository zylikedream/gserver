package gxyactor

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxylog"

	"ergo.services/ergo/gen"
)

// ActorTimer 是 actor 内的定时器门面。
//
// 三种定时都用运行时能力实现,触发都经邮箱回到本进程:
//
//   - 周期与一次性:运行时原语,进程终止即失效;
//   - 每日定点:运行时的 cron 调度器(节点级),由本 actor 注册与注销。
//
// 触发回到邮箱意味着回调在 actor 上下文中串行执行,不需要额外的并发保护;
// 且不会在 actor 已经终止后继续执行。
type ActorTimer struct {
	actor         *Actor
	callbackFuncs map[string]func(ctx context.Context)
	cancels       []gen.CancelFunc
	// cronJobs 记录本 actor 注册的 cron 任务名,终止时逐个注销,
	// 避免 actor 消失后节点上仍留有指向死进程的任务。
	cronJobs []gen.Atom
}

func newActorTimer(a *Actor) *ActorTimer {
	return &ActorTimer{
		actor:         a,
		callbackFuncs: make(map[string]func(ctx context.Context)),
	}
}

// AddTick 注册周期定时。
func (s *ActorTimer) AddTick(name string, interval time.Duration, fn func(ctx context.Context)) {
	s.callbackFuncs[name] = fn
	s.every(name, interval)
}

// AddOnce 注册一次性定时。
func (s *ActorTimer) AddOnce(name string, delay time.Duration, fn func(ctx context.Context)) {
	s.callbackFuncs[name] = fn
	s.after(name, delay)
}

// AddDaily 注册每日定点定时。hour 为本地时间的小时数,到点触发。
func (s *ActorTimer) AddDaily(name string, hour int, fn func(ctx context.Context)) {
	s.callbackFuncs[name] = fn
	jobName := s.jobName(name)
	spec := fmt.Sprintf("0 %d * * *", hour)
	err := s.actor.Node().Cron().AddJob(gen.CronJob{
		Name:   jobName,
		Spec:   spec,
		Action: gen.CreateCronActionMessage(s.actor.PID(), gen.MessagePriorityNormal),
	})
	if err != nil {
		gxylog.Error(s.actor.Ctx, "register daily timer failed",
			gxylog.Str("timer", string(jobName)), gxylog.Str("spec", spec), gxylog.Err(err))
		return
	}
	s.cronJobs = append(s.cronJobs, jobName)
}

// ActiveCron 执行到点的 cron 回调。由基类在收到 cron 消息时调用。
func (s *ActorTimer) ActiveCron(ctx context.Context, job gen.Atom) {
	name := string(job)
	if idx := lastIndexByte(name, '@'); idx >= 0 {
		name = name[:idx]
	}
	if fn, ok := s.callbackFuncs[name]; ok {
		fn(ctx)
	}
}

// Cancel 取消指定名字的定时回调。已排入运行时的触发不再执行。
func (s *ActorTimer) Cancel(ctx context.Context, name string) {
	delete(s.callbackFuncs, name)
}

// Stop 停止全部定时。由基类在终止路径调用。
func (s *ActorTimer) Stop(ctx context.Context) {
	for _, cancel := range s.cancels {
		cancel()
	}
	s.cancels = nil

	for _, job := range s.cronJobs {
		if err := s.actor.Node().Cron().RemoveJob(job); err != nil {
			gxylog.Warn(ctx, "remove daily timer failed",
				gxylog.Str("timer", string(job)), gxylog.Err(err))
		}
	}
	s.cronJobs = nil
	s.callbackFuncs = make(map[string]func(ctx context.Context))
}

// every 用运行时原语注册周期触发,到期经邮箱投递。
func (s *ActorTimer) every(name string, interval time.Duration) {
	if interval <= 0 {
		gxylog.Error(s.actor.Ctx, "timer interval must be positive", gxylog.Str("timer", name))
		return
	}
	cancel, err := s.actor.SendEvery(s.actor.PID(), ActorTimerMsg{Name: name}, interval)
	if err != nil {
		gxylog.Error(s.actor.Ctx, "register tick timer failed", gxylog.Str("timer", name), gxylog.Err(err))
		return
	}
	s.cancels = append(s.cancels, cancel)
}

// after 用运行时原语注册一次性触发,到期经邮箱投递。
func (s *ActorTimer) after(name string, delay time.Duration) {
	if delay <= 0 {
		gxylog.Error(s.actor.Ctx, "timer delay must be positive", gxylog.Str("timer", name))
		return
	}
	cancel, err := s.actor.SendAfter(s.actor.PID(), ActorTimerMsg{Name: name}, delay)
	if err != nil {
		gxylog.Error(s.actor.Ctx, "register once timer failed", gxylog.Str("timer", name), gxylog.Err(err))
		return
	}
	s.cancels = append(s.cancels, cancel)
}

// Active 执行到期的周期或一次性回调。
func (s *ActorTimer) Active(ctx context.Context, msg ActorTimerMsg) {
	if fn, ok := s.callbackFuncs[msg.Name]; ok {
		fn(ctx)
	}
}

// jobName 生成 cron 任务名。cron 是节点级的,而每个 actor 都要注册自己的
// 每日任务,因此任务名必须带上 actor 身份以避免冲突。
func (s *ActorTimer) jobName(name string) gen.Atom {
	owner := "pid" + s.actor.PID().String()
	if n := s.actor.Name(); n != "" {
		owner = string(n)
	}
	return gen.Atom(name + "@" + owner)
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}
