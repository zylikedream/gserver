package gxyactor

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"

	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxytrace"
	"gserver/protocol/pb"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

// runtimeActor 是**唯一**交给运行时的对象,也是业务与运行时之间的接缝(见 ADR 0017)。
//
// 它内嵌运行时的行为基类,因此运行时的每次回调都落在它这里,再由它转给业务。
// 业务永远看不到本类型:
//
//   - 覆写不到这里的回调 —— 门面机制(定时消息分流、消息还原、终止原因解释)
//     不可能被业务关掉,这是编译期结论而非约定;
//   - 拿不到内嵌的运行时进程能力 —— 跨节点封装无法被绕过。
//
// 编排也归这里:同步段与异步段的衔接、分派目标的注册时机、监视通知的翻译。
// 业务只声明"异步段做什么",不负责"什么时候做"。
type runtimeActor struct {
	act.Actor // ergoactor

	base     *BaseActor
	behavior ActorBehavior
}

// ActorFactory 把业务构造器适配成运行时的进程工厂。
//
// kind 在此写入实例,且是**唯一**的写入点——业务构造器不认识它:
//
//   - 命名 actor(经登记或按名创建):kind 是注册表的键。这正是"注册名与能力名
//     不一致"不可能的由来——不一致不会报错,只会让实例注册到查不到的名字下,
//     于是被反复重建;
//   - 无名 actor(会话):kind 只作类别标签用。见 Actor.kind 的说明。
//
// 同时按 kind 建立日志上下文:此后本 actor 的每条日志都带该字段。
// 无条件重建是刻意的——构造器给的空标签没有信息,不该留着。
func ActorFactory(kind string, ctor ActorConstructor) gen.ProcessFactory {
	return func() gen.ProcessBehavior {
		behavior := ctor()
		base := behavior.base()
		base.kind = kind
		base.Ctx = gxylog.NewContext(context.Background(), kind)
		return newRuntimeActor(base, behavior)
	}
}

func newRuntimeActor(base *BaseActor, behavior ActorBehavior) *runtimeActor {
	return &runtimeActor{base: base, behavior: behavior}
}

// messageCtx 派生**本条消息**的 ctx:父是本 actor 的 ctx(带 kind 等常量字段),
// 再挂上运行时刚为这条消息激活的链路标识(见 gxytrace.ContextWithErgoTrace)。
//
// 于是业务入口拿到的是消息级 ctx:日志自动带该链路的 trace_id,而 actor 级字段
// 仍留在 base.Ctx 里;没有入站消息的入口(Init/Terminate)用 base.Ctx。
func (r *runtimeActor) messageCtx() context.Context {
	return gxytrace.ContextWithErgoTrace(r.base.Ctx, r.PropagatingTrace())
}

// ===== 运行时回调:只出现在本类型上 =====

// Init 是运行时回调。
//
// 顺序即语义:先备环境,再让业务绑定标识,再取归属(实体层),最后驱动异步段。
// 同步段必须够快且不碰数据库——耗时加载属于异步段。
func (r *runtimeActor) Init(args ...any) error {
	r.base.rt = r
	r.base.pid = pidFromLocal(r.PID())
	r.base.initArgs = args
	r.base.startup()

	// 同步段没有入站消息,用 actor 级 ctx(运行时不在此阶段开链路)。
	if err := r.behavior.Init(r.base.Ctx, args...); err != nil {
		return err
	}

	// 分派目标是业务对象自身,必须在处理任何消息之前注册。
	// 任何 actor 都可按消息类型分派,与是否承载实体无关。
	r.base.msgHandler.AddHandler(r.behavior)

	// 实体层在同步段取归属:此时尚未处理任何业务消息(不变量 #3)。
	if e := entityOf(r.behavior); e != nil {
		if _, err := e.acquireOwnership(); err != nil {
			return err
		}
	}

	// 编排归门面:业务只声明异步段做什么,不必自己发消息。
	// 在此投递,邮箱为空,因此它必然先于任何外部业务消息被处理。
	return r.SendPID(r.PID(), actorInitMsg{})
}

// HandleMessage 是运行时回调(异步)。**业务不覆写本方法**——它看不到本类型。
//
// 门面在此承担与业务无关的机制:定时消息分流(触发经邮箱投递,必须在这里回到
// 已注册的回调)、异步初始化驱动、跨节点消息还原。业务处理从 HandleMessage 进入;
// 两件事不共用一个名字,覆写业务处理不会连机制一起关掉。
func (r *runtimeActor) HandleMessage(from gen.PID, message any) error {
	// 机制分流:这些消息业务看不到,也不该看到。
	switch msg := message.(type) {
	case actorInitMsg:
		ctx := r.messageCtx()
		if err := r.behavior.AsyncInit(ctx); err != nil {
			gxylog.Error(ctx, "async init failed", gxylog.Err(err))
			return err
		}
		return r.base.stopReason()
	case ActorTimerMsg:
		r.base.timer.Active(r.base.Ctx, msg)
		// 回调里可能调用过 Stop:必须把终止原因交回运行时,
		// 否则"回调返回后终止"的契约在定时路径上不成立(登出回收与落盘失败停止都会失效)。
		return r.base.stopReason()
	case gen.MessageCron:
		r.base.timer.ActiveCron(r.base.Ctx, msg.Job)
		return r.base.stopReason()
	case *pb.ActorStop:
		// 按消息请求停止:调用方(角色、网关)用它结束会话。
		// 会话在本分支不承载实体,停止只是终止自身。
		//
		// 原因经协议传的是字符串,空串即"没有原因"——正常结束(如连接干净关闭)。
		// 必须让它落到 Stop(nil):终止原因非 nil 时运行时一律记
		// "process terminated abnormally" 的 error,于是每次玩家正常下线都会刷一条
		// 错误(见 Actor.stopReason)。
		if msg.Reason == "" {
			r.base.Stop(nil)
		} else {
			r.base.Stop(errors.New(msg.Reason))
		}
		return r.base.stopReason()
	case gen.MessageDownPID:
		// 监视通知翻译成门面身份后交给业务:业务侧不出现运行时消息类型。
		r.behavior.HandleDown(r.messageCtx(), PidFromRuntime(msg.PID))
		return r.base.stopReason()
	}

	// 本条消息的 ctx:业务入口与门面日志共用它,日志因此带上当前链路的 trace_id。
	ctx := r.messageCtx()

	r.base.sender = pidFromLocal(from)
	decoded, err := UnwrapWire(message)
	if err != nil {
		// 报文不合法不足以让 actor 死掉:丢掉这一条,继续服务。
		gxylog.Error(ctx, "decode wire message failed", gxylog.Err(err))
		return nil
	}

	// 断言必然成立:业务对象经内嵌基类获得默认入口(见 Actor.HandleMessage)。
	start := time.Now()
	rsp, err := r.behavior.HandleMessage(ctx, decoded)
	gxymetrics.ActorMessages.WithLabelValues(r.base.kind).Inc()
	// 带上 exemplar:从慢样本能直接跳到对应 trace。
	gxymetrics.ObserveWithTrace(ctx, gxymetrics.ActorMessageDuration.WithLabelValues(r.base.kind), time.Since(start).Seconds())
	if err != nil {
		gxylog.Error(ctx, "handle msg failed", gxylog.Any("payload", decoded), gxylog.Err(err))
		return err
	}
	// 异步消息的应答是处理函数的返回值;没有返回值表示不需要应答。
	if rsp != nil && from != (gen.PID{}) {
		r.base.sendReply(from, rsp)
	}
	return r.base.stopReason()
}

// HandleCall 是运行时回调(同步)。**业务不覆写本方法**。
//
// 业务错误走响应通道而不作终止原因:运行时的契约是"回调返回非 nil error 即终止
// 本进程",而业务失败不应终止进程(见 invariants #9)。
func (r *runtimeActor) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	// 本条消息的 ctx:业务入口与门面日志共用它。
	ctx := r.messageCtx()

	r.base.sender = pidFromLocal(from)
	decoded, err := UnwrapWire(request)
	if err != nil {
		return nil, err
	}

	// 业务入口经内嵌基类获得默认实现(见 Actor.HandleCall),此处直接调用。
	start := time.Now()
	rsp, err := r.behavior.HandleCall(ctx, decoded)
	gxymetrics.ActorMessages.WithLabelValues(r.base.kind).Inc()
	gxymetrics.ObserveWithTrace(ctx, gxymetrics.ActorMessageDuration.WithLabelValues(r.base.kind), time.Since(start).Seconds())
	switch {
	case err != nil:
		gxylog.Error(ctx, "handle rpc msg failed", gxylog.Any("payload", decoded), gxylog.Err(err))
		r.base.sendResponse(from, ref, &pb.ActorError{Reason: err.Error()})
	case rsp == nil:
		// 同步请求必须给出应答:业务既没有返回结果,也没有导出的能力自行回复
		// (sendResponse/sendReply 都是包内方法)。走到这里说明某个 handler 漏写了
		// 应答——静默不发会让调用方一直等到超时,把这个 bug 藏起来。
		gxylog.Error(ctx, "handle rpc msg returned no response", gxylog.Any("payload", decoded))
		r.base.sendResponse(from, ref, &pb.ActorError{Reason: "no response for sync request"})
	default:
		r.base.sendResponse(from, ref, rsp)
	}
	return nil, r.base.stopReason()
}

// Terminate 是运行时回调。
//
// 顺序即语义:先让业务完成最终落盘,再由实体层释放归属(不变量 #4),最后收拾
// 运行环境。颠倒会让新持有者推进世代,使本次落盘被拒。
func (r *runtimeActor) Terminate(reason error) {
	// 终止路径没有入站消息:业务终止与归属释放共用 actor 级 ctx。
	r.behavior.Terminate(r.base.Ctx, reason)
	if e := entityOf(r.behavior); e != nil {
		e.dropOwnership(r.base.Ctx)
	}
	r.base.shutdown()
	r.base.rt = nil
}
