package gxyactor

import (
	"context"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxytrace"
	"gserver/core/gxyutil"
	"gserver/protocol/pb"

	"ergo.services/ergo/gen"
	"github.com/cockroachdb/errors"
	"github.com/gogf/gf/v2/errors/gerror"
)

// ActorConstructor 构造一个业务对象。
//
// 业务只交出"怎么造自己",不交出"自己叫什么名字":能力名由登记处按注册表的键
// 写入,构造器不认识它(见 ActorFactory)。
type ActorConstructor func() Business

// Business 是业务对象的类型约束,由内嵌门面基类的结构体满足。
//
// 方法未导出:包外类型无法自行实现它,只能通过**内嵌门面基类**获得。因此
// "登记一个业务对象"这件事无法被伪造,门面不必再声明一个可被冒名的接口。
//
// 业务对象不被运行时看见——它与运行时之间的适配在 runtime_actor.go(见 ADR 0017)。
type Business interface {
	actorBase() *Actor
}

// 业务入口。业务按需实现,未实现即不处理该类事件。
//
// 这些名字由业务独占:运行时回调只出现在门面的适配对象上,而业务对象不会被
// 当作运行时的行为对象交给运行时,因此两组名字不可能相撞(见 ADR 0017)。
type (
	// initializer 同步初始化段:只做标识绑定与内存校验,必须快速返回。
	initializer interface{ Init(args ...any) error }

	// asyncInitializer 异步初始化段:耗时加载放这里,
	// 由门面在同步段之后自投消息驱动,业务不自己发消息。
	asyncInitializer interface{ AsyncInit() error }

	// messageHandler 异步消息入口。
	messageHandler interface{ HandleMessage(msg any) (any, error) }

	// callHandler 同步请求入口。
	callHandler interface{ HandleCall(msg any) (any, error) }

	// downHandler 被监视目标终止的通知入口。
	downHandler interface{ HandleDown(pid PID) }

	// terminator 终止路径:先做最终落盘。归属释放在此之后由门面执行。
	terminator interface{ Terminate(err error) }
)

// actorInitMsg 是门面自投的异步初始化消息。
//
// 用途:把耗时加载放到同步初始化返回之后。投递时邮箱为空,因此它必然先于任何
// 外部业务消息被处理——这条保证由门面独占,业务不发送也看不到它。
type actorInitMsg struct{}

// ActorTimerMsg 定时器到期的内部消息,只携带定时名。
// 触发时刻由接收方在执行时确定,不需要跨进程传递。
type ActorTimerMsg struct {
	Name string
}

// Actor 是业务 actor 的基类:业务内嵌它以取得"运行时没给而项目需要"的能力。
//
// 它**不内嵌任何运行时类型**。因此业务:
//
//   - 拿不到运行时的原生发送、派生、命名等能力,跨节点封装无法被绕过;
//   - 不可能被当作运行时的行为对象交给运行时,也就无法覆写掉门面机制;
//   - 类型上不出现运行时类型,更换运行时的触碰面回到门面。
//
// 见 ADR 0017。
type Actor struct {
	// Ctx 是带日志与追踪上下文的 context。
	Ctx context.Context

	// rt 是运行时句柄。Go 的内嵌是静态分派,基类无法从内部调回业务覆写,
	// 因此业务回调统一由 rt 分发;它也是基类访问运行时能力的唯一入口。
	rt *runtimeActor

	kind string
	self PID

	// msgHandler 按消息类型把消息路由到业务方法。
	//
	// 它属于"是个 actor"这一层,与"是否承载实体"无关:任何 actor 都可以只声明
	// "哪类消息由哪个方法处理",不必参与所有权协议(见 ADR 0015)。
	msgHandler *gxyutil.MsgHandler

	// initArgs 是本次创建的初始化参数,由门面在业务同步段之前记录:
	// 实体层据此取出标识(见 EntityActor.acquireOwnership)。
	initArgs []any

	// timer 懒创建。
	timer *ActorTimer

	// sender 是当前正在处理的消息的发送者,由门面在分派前记录。
	// 反射分派无法传递发送者,业务用 Sender() 读取。
	sender PID

	// stopErr 记录终止原因;stopRequested 区分"未请求终止"与"正常终止"。
	stopErr       error
	stopRequested bool
}

// NewActor 创建门面基类。承载持久实体的 actor 应内嵌 EntityActor。
//
// 不传自身,也不传能力名:自身由门面持有,能力名由登记处按注册表的键一次写入。
func NewActor() *Actor {
	return &Actor{
		Ctx:        gxylog.NewContext(context.Background(), ""),
		msgHandler: gxyutil.NewMsgHandler(),
	}
}

// actorBase 使内嵌本类型的业务对象满足 Business。
func (a *Actor) actorBase() *Actor { return a }

// biz 返回外层业务对象(工厂返回的那个结构体)。
//
// 必须经运行时句柄取:Go 的内嵌是静态分派,基类内部无法直接调到外层的覆写。
// 它是"按需发现业务实现了哪些入口"的唯一来源。
func (a *Actor) biz() Business {
	if a.rt == nil {
		return nil
	}
	return a.rt.biz
}

// ActorKind 返回该 actor 的能力名。
func (a *Actor) ActorKind() string { return a.kind }

// ===== 消息分派 =====

// HandleMessage 是默认的异步消息入口:按消息类型反射分派。
// 业务覆写它以自行路由(如按来源分流)时,不再走分派表。
func (a *Actor) HandleMessage(msg any) (any, error) { return a.dispatchTo(msg) }

// HandleCall 是默认的同步请求入口:按消息类型反射分派。
func (a *Actor) HandleCall(msg any) (any, error) { return a.dispatchTo(msg) }

// dispatcher 由业务实现以接管消息分派(如加限流、埋点)。
type dispatcher interface {
	Dispatch(message any) (any, error)
}

// dispatchTo 把消息交给分派目标:业务覆写了 Dispatch 就用业务的,
// 否则按消息类型查找已注册的处理方法。
func (a *Actor) dispatchTo(message any) (any, error) {
	if d, ok := a.biz().(dispatcher); ok {
		return d.Dispatch(message)
	}
	// 没有对应处理器时视为"本 actor 不处理这类消息",不是错误:每个 actor 都经
	// 内嵌基类获得本入口,把"没有这条路由"当致命会把无关消息变成进程终止。
	if a.msgHandler.GetMethodMeta(message) == nil {
		return nil, nil
	}
	return a.DispatchDefault(message)
}

// DispatchDefault 按消息类型反射分派,没有对应处理器时返回错误。
// 业务覆写 Dispatch 时应在其中调用本方法,以免递归。
//
// 与入口默认分派的差别在于**没有处理器时返回错误**:显式调用它的业务正是要
// 判断"有没有这条路由"(如协议错误处理)。
func (a *Actor) DispatchDefault(message any) (any, error) {
	return a.msgHandler.CallWithMsg(a.Ctx, message)
}

// AddMsgHandler 为指定对象注册消息分派,返回注册到的方法。
func (a *Actor) AddMsgHandler(handler any, prefix ...string) []*gxyutil.MethodMeta {
	return a.msgHandler.AddHandler(handler, prefix...)
}

// ===== 门面内部:由运行时适配对象在生命周期两端调用 =====

// startup 准备运行所需的环境。在业务同步段之前调用。
func (a *Actor) startup() {
	// 让本 actor 发起的消息按配置比例开启链路追踪(ADR 0013)。
	// 必须设在进程上:运行时发消息时看的是进程级采样器,节点级只对"节点自身
	// 发起"的消息生效——只设节点级会得到一个永远没有 span 的空追踪。
	if rate := gxytrace.SampleRate(); rate > 0 {
		if err := a.rt.SetTracingSampler(gen.TracingSamplerRatio(rate)); err != nil {
			gxylog.Warn(a.Ctx, "set process tracing sampler failed", gxylog.Err(err))
		}
	}
	gxymetrics.ActorActiveCount.WithLabelValues(a.kind).Inc()
}

// shutdown 收拾运行环境。在业务终止路径之后调用。
func (a *Actor) shutdown() {
	if a.timer != nil {
		a.timer.Stop(a.Ctx)
	}
	gxymetrics.ActorActiveCount.WithLabelValues(a.kind).Dec()
}

// stopReason 返回应交给运行时的终止原因;未请求终止时返回 nil。
// 运行时的契约是"回调返回非 nil 即终止",因此 Stop(nil) 需转成正常终止原因。
func (a *Actor) stopReason() error {
	if !a.stopRequested {
		return nil
	}
	if a.stopErr != nil {
		return a.stopErr
	}
	return gen.TerminateReasonNormal
}

// prepareOutbound 决定发往某节点的消息是否需要装信封。
// 只有跨节点消息需要;本机投递保持零拷贝。节点未初始化时原样返回。
func (a *Actor) prepareOutbound(message any, node string) (any, error) {
	if app == nil || app.node == nil {
		return message, nil
	}
	return app.prepareOutbound(message, node)
}

// sendReply 把异步消息的应答回给发送者;跨节点时自动装信封。
func (a *Actor) sendReply(to gen.PID, message any) {
	out, err := a.prepareOutbound(message, string(to.Node))
	if err != nil {
		gxylog.Warn(a.Ctx, "prepare reply failed", gxylog.Err(err))
		return
	}
	if err := a.rt.Send(to, out); err != nil {
		// 应答没送到不该让一个正在服务其它请求的 actor 死掉,只记录。
		gxylog.Warn(a.Ctx, "reply to sender failed", gxylog.Err(err))
	}
}

// sendResponse 把同步请求的结果或业务失败回给调用方。
//
// 必须走响应通道(带上调用方的 ref):用普通发送回包,调用方的 Call 收不到结果
// 会超时,而迟到的应答会被当成一条新消息投递,在调用方触发"无对应处理器"。
func (a *Actor) sendResponse(to gen.PID, ref gen.Ref, message any) {
	out, err := a.prepareOutbound(message, string(to.Node))
	if err != nil {
		gxylog.Warn(a.Ctx, "prepare response failed", gxylog.Err(err))
		return
	}
	if err := a.rt.SendResponse(to, ref, out); err != nil {
		gxylog.Warn(a.Ctx, "reply failed", gxylog.Err(err))
	}
}

// ===== 业务常用能力 =====

// Self 返回本 actor 的地址。
func (a *Actor) Self() PID { return a.self }

// Sender 返回当前正在处理的消息的发送者;不在回调中时为零值。
func (a *Actor) Sender() PID { return a.sender }

// SendTo 向目标 actor 发送消息;跨节点时自动装信封。
func (a *Actor) SendTo(pid PID, message any) error {
	target := pid.target()
	if target == nil {
		return gerror.New("send to empty pid")
	}
	out, err := a.prepareOutbound(message, pid.Node())
	if err != nil {
		return err
	}
	return a.rt.Send(target, out)
}

// Call 同步调用目标 actor;跨节点时自动装信封。
//
// 对端把业务失败作为错误载荷返回(invariants #9),这里还原成 error——调用方
// 才能用统一的 if err != nil 处理。这一步不能省:调用方拿到的是一个"调用成功
// 但内容是失败"的结果,不还原就只能靠类型断言去猜,猜错即 panic。
//
// 与"调用本身失败"区分:超时、对端不存在、对端拒绝调用由运行时直接返回 error。
func (a *Actor) Call(pid PID, message any, timeout time.Duration) (any, error) {
	target := pid.target()
	if target == nil {
		return nil, gerror.New("call on empty pid")
	}
	out, err := a.prepareOutbound(message, pid.Node())
	if err != nil {
		return nil, err
	}
	result, err := a.rt.CallWithTimeout(target, out, int(timeout.Seconds()))
	if err != nil {
		return nil, err
	}
	rsp, err := UnwrapWire(result)
	if err != nil {
		return nil, err
	}
	if aerr, ok := rsp.(*pb.ActorError); ok {
		return nil, errors.New(aerr.Reason)
	}
	return rsp, nil
}

// Watch 监视目标 actor;它终止时本 actor 会经 HandleDown 收到通知。
// 注意:该通知早于对端终止回调完成,不得作为"对端已落盘"的依据。
func (a *Actor) Watch(pid PID) error {
	target := pid.target()
	if target == nil {
		return gerror.New("watch on empty pid")
	}
	return a.rt.Monitor(target)
}

// Unwatch 取消监视。
func (a *Actor) Unwatch(pid PID) error {
	target := pid.target()
	if target == nil {
		return gerror.New("unwatch on empty pid")
	}
	return a.rt.Demonitor(target)
}

// SetTracingSpanAttribute 为当前处理的消息附加追踪属性。
// 运行时的追踪自动覆盖消息链路,属性用于补充业务上下文。
func (a *Actor) SetTracingSpanAttribute(key, value string) {
	a.rt.SetTracingSpanAttribute(key, value)
}

// Timer 返回本 actor 的定时器。
func (a *Actor) Timer() *ActorTimer {
	if a.timer == nil {
		a.timer = newActorTimer(a)
	}
	return a.timer
}

// SetLogValue 为日志上下文附加字段。
func (a *Actor) SetLogValue(key string, val any) *Actor {
	a.Ctx = gxylog.WithValue(a.Ctx, key, val)
	return a
}

// Stop 请求终止本 actor。终止在本次回调返回后发生;err 为 nil 表示正常终止。
func (a *Actor) Stop(err error) {
	a.stopRequested = true
	a.stopErr = err
}

// StopRequested 报告是否已请求终止。
func (a *Actor) StopRequested() bool { return a.stopRequested }
