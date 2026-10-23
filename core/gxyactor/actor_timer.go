package gxyactor

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxytrace"

	"ergo.services/ergo/gen"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
//
// 名字是唯一的:同名重复注册只更新回调,不会再注册一个运行时定时器。
// 登录这类可重复路径因此不会按次数累积触发;Cancel 会真正取消运行时定时器,
// 而不是只让回调变成空操作。
type ActorTimer struct {
	actor         *BaseActor
	callbackFuncs map[string]func(ctx context.Context)
	// timers 记录已注册的运行时定时器(周期与一次性共用同一名字空间)。
	timers map[string]gen.CancelFunc
	// ones 标记一次性定时:触发后清掉名字,允许下次用同名重新注册。
	ones map[string]struct{}
	// cronJobs 记录本 actor 注册的 cron 任务名,终止时逐个注销,
	// 避免 actor 消失后节点上仍留有指向死进程的任务。
	cronJobs []gen.Atom
}

func newActorTimer(a *BaseActor) *ActorTimer {
	return &ActorTimer{
		actor:         a,
		callbackFuncs: make(map[string]func(ctx context.Context)),
		timers:        make(map[string]gen.CancelFunc),
		ones:          make(map[string]struct{}),
	}
}

// AddTick 注册周期定时。
// 同名定时已存在时只替换回调、沿用原周期——重复调用是幂等的。
func (s *ActorTimer) AddTick(name string, interval time.Duration, fn func(ctx context.Context)) {
	s.callbackFuncs[name] = fn
	if _, exists := s.timers[name]; exists {
		return
	}
	if interval <= 0 {
		gxylog.Error(s.actor.Ctx, "timer interval must be positive", gxylog.Str("timer", name))
		return
	}
	cancel, err := s.actor.rt.SendEvery(s.actor.rt.PID(), ActorTimerMsg{Name: name}, interval)
	if err != nil {
		gxylog.Error(s.actor.Ctx, "register tick timer failed", gxylog.Str("timer", name), gxylog.Err(err))
		return
	}
	s.timers[name] = cancel
}

// AddOnce 注册一次性定时。
// 同名定时尚未触发时先取消旧的再重新注册(重置计时)。
func (s *ActorTimer) AddOnce(name string, delay time.Duration, fn func(ctx context.Context)) {
	s.callbackFuncs[name] = fn
	if cancel, exists := s.timers[name]; exists {
		cancel()
		delete(s.timers, name)
		delete(s.ones, name)
	}
	if delay <= 0 {
		gxylog.Error(s.actor.Ctx, "timer delay must be positive", gxylog.Str("timer", name))
		return
	}
	cancel, err := s.actor.rt.SendAfter(s.actor.rt.PID(), ActorTimerMsg{Name: name}, delay)
	if err != nil {
		gxylog.Error(s.actor.Ctx, "register once timer failed", gxylog.Str("timer", name), gxylog.Err(err))
		return
	}
	s.timers[name] = cancel
	s.ones[name] = struct{}{}
}

// AddDaily 注册每日定点定时。hour 为本地时间的小时数,到点触发。
func (s *ActorTimer) AddDaily(name string, hour int, fn func(ctx context.Context)) {
	s.callbackFuncs[name] = fn
	jobName := s.jobName(name)
	spec := fmt.Sprintf("0 %d * * *", hour)
	err := s.actor.rt.Node().Cron().AddJob(gen.CronJob{
		Name:   jobName,
		Spec:   spec,
		Action: gen.CreateCronActionMessage(s.actor.rt.PID(), gen.MessagePriorityNormal),
	})
	if err != nil {
		gxylog.Error(s.actor.Ctx, "register daily timer failed",
			gxylog.Str("timer", string(jobName)), gxylog.Str("spec", spec), gxylog.Err(err))
		return
	}
	s.cronJobs = append(s.cronJobs, jobName)
}

// Active 执行到期的周期或一次性回调。
// 一次性定时先摘除名字再执行回调,回调内部可以安全地重新注册同名定时。
func (s *ActorTimer) Active(ctx context.Context, msg ActorTimerMsg) {
	fn, ok := s.callbackFuncs[msg.Name]
	if !ok {
		return
	}
	if _, one := s.ones[msg.Name]; one {
		delete(s.ones, msg.Name)
		delete(s.timers, msg.Name)
		delete(s.callbackFuncs, msg.Name)
	}
	ctx, span := s.startTimerSpan(ctx, msg.Name)
	defer span.End()
	fn(ctx)
}

// ActiveCron 执行到点的 cron 回调。由基类在收到 cron 消息时调用。
func (s *ActorTimer) ActiveCron(ctx context.Context, job gen.Atom) {
	name := string(job)
	if idx := lastIndexByte(name, '@'); idx >= 0 {
		name = name[:idx]
	}
	fn, ok := s.callbackFuncs[name]
	if !ok {
		return
	}
	ctx, span := s.startTimerSpan(ctx, name)
	defer span.End()
	fn(ctx)
}

// startTimerSpan 为一次定时回调创建独立 span。
//
// 定时消息由运行时自发,没有入站链路,因此这里起一条新 trace:回调里的日志
// 与下游调用因此能关联到同一次 tick。是否产生 span 只看全局采样开关
// (SampleRate > 0)——定时器频率是分钟级,不再按比例二次采样。
func (s *ActorTimer) startTimerSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	if gxytrace.SampleRate() <= 0 {
		return ctx, trace.SpanFromContext(ctx)
	}
	attrs := []attribute.KeyValue{attribute.String("timer", name)}
	if s.actor != nil {
		attrs = append(attrs,
			attribute.String("actor", s.actor.pid.String()),
			attribute.String("actor.kind", s.actor.kind),
		)
	}
	return otel.Tracer("gxyactor.timer").Start(ctx, "timer."+name, trace.WithAttributes(attrs...))
}

// Cancel 取消指定名字的定时:删除回调并取消运行时定时器。
// 对一次性定时,未触发前的取消同时移除名字,允许之后重新注册。
func (s *ActorTimer) Cancel(ctx context.Context, name string) {
	delete(s.callbackFuncs, name)
	if cancel, ok := s.timers[name]; ok {
		cancel()
		delete(s.timers, name)
	}
	delete(s.ones, name)
}

// Stop 停止全部定时。由基类在终止路径调用。
func (s *ActorTimer) Stop(ctx context.Context) {
	for name, cancel := range s.timers {
		cancel()
		delete(s.timers, name)
	}
	s.ones = make(map[string]struct{})

	for _, job := range s.cronJobs {
		if err := s.actor.rt.Node().Cron().RemoveJob(job); err != nil {
			gxylog.Warn(ctx, "remove daily timer failed",
				gxylog.Str("timer", string(job)), gxylog.Err(err))
		}
	}
	s.cronJobs = nil
	s.callbackFuncs = make(map[string]func(ctx context.Context))
}

// jobName 生成 cron 任务名。cron 是节点级的,而每个 actor 都要注册自己的
// 每日任务,因此任务名必须带上 actor 身份以避免冲突。
func (s *ActorTimer) jobName(name string) gen.Atom {
	owner := "pid" + s.actor.pid.String()
	if n := s.actor.rt.Name(); n != "" {
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
