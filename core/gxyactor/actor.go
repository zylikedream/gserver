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
type ActorConstructor func() ActorBehavior

// ActorBehavior 是 actor 对象的契约:工厂返回的就是它,门面通过它调用业务入口。
//
// 六个入口在 BaseActor 上都有默认实现(什么都不做),业务按需覆写即可接管——
// 覆写谁就接管谁,没覆写的走默认;门面因此不必再靠断言逐个"发现"入口。
//
// 这些名字由业务独占:运行时回调只出现在门面的适配对象上,而业务对象不会被
// 当作运行时的行为对象交给运行时,因此两组名字不可能相撞(见 ADR 0017)。
type ActorBehavior interface {
	Init(args ...any) error
	AsyncInit() error
	HandleMessage(msg any) (any, error)
	HandleCall(msg any) (any, error)
	HandleDown(pid PID)
	Terminate(err error)
	base() *BaseActor
}

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

// BaseActor 是业务 actor 的基类:业务内嵌它以取得"运行时没给而项目需要"的能力。
type BaseActor struct {
	// Ctx 是带日志与追踪上下文的 context。
	Ctx context.Context

	// rt 是运行时句柄。Go 的内嵌是静态分派,基类无法从内部调回业务覆写,
	// 因此业务回调统一由 rt 分发;它也是基类访问运行时能力的唯一入口。
	rt *runtimeActor

	// kind 是该 actor 的**类别**,同时扮演两种角色,义务只在其中一种上产生:
	//
	//  1. 类别标签(所有 actor):指标维度 `actor_active_count` / `actor_messages_total`
	//     与日志字段 `mod`(见 gxylog.NewContext)。看板按它聚合,没有它这类 actor
	//     会从面板里静默消失。
	//  2. 能力名(仅承载实体时):它是归属命名空间(`<kind>/<id>` 与 Redis 归属键)、
	//     本地构造表 `activatorManager.kinds`、服务发现目录三者共用的键。
	//     此时它有一个硬义务——**必须等于登记表的键**,由 ActorFactory 按注册名
	//     一次写入,不由实例自行推断。承载实体的 actor 若拿不到标识,初始化即失败。
	//
	// 为什么需要第二种角色(Erlang 里进程没有 kind):调用方在本节点说"给我
	// <kind>/<id>",而本节点可以没有它,需要到别的节点上按需造出来,于是必须有一张
	// "能力 → 构造函数"的集群目录;同时 kind/id 是跨节点单写者的命名空间。
	// 不承载实体的 actor 不需要这两件事,因此只承担第一种角色。
	kind string
	pid  PID

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

// NewBaseActor 创建门面基类。承载持久实体的 actor 应内嵌 EntityActor。
//
// 不传自身,也不传能力名:自身由门面持有,能力名由登记处按注册表的键一次写入。
func NewBaseActor() *BaseActor {
	return &BaseActor{
		// Ctx 在此给出有效默认值:它是每个 actor 都用到的依赖(日志、追踪、DB),
		// 不能让"还没经工厂接线"这个窗口里存在 nil——直接构造(测试、嵌入式调用)
		// 的路径不经过 ActorFactory。工厂会无条件重建它并带上 kind 标签。
		Ctx:        gxylog.NewContext(context.Background(), ""),
		msgHandler: gxyutil.NewMsgHandler(),
	}
}

func (b *BaseActor) Init(args ...any) error {
	return nil
}

func (b *BaseActor) AsyncInit() error {
	return nil
}

// base 使内嵌本类型的业务对象满足 ActorBehavior。
//
// **必须未导出**:包外类型因此无法只手写一个方法就"变成"actor 对象——只能经
// 内嵌门面基类获得这个身份,"登记一个业务对象"这件事才无法被伪造。跨包的
// 方法提升照常生效(业务结构体内嵌基类即满足本接口),门面也只需在本包内调用它。
func (b *BaseActor) base() *BaseActor {
	return b
}

// ===== 消息分派 =====

// HandleMessage 是默认的异步消息入口:按消息类型反射分派。
// 业务覆写它以自行路由(如按来源分流)时,不再走分派表。
//
// 没有对应处理器时返回 (nil, nil):异步消息没有调用方在等回包,把"没有这条路由"
// 当致命会让一条发错地方的消息终止整个 actor(见 TestUnroutedMessageDoesNotTerminateActor)。
func (b *BaseActor) HandleMessage(msg any) (any, error) {
	if b.msgHandler.GetMethodMeta(msg) == nil {
		return nil, nil
	}
	return b.dispatch(msg)
}

// HandleCall 是默认的同步请求入口:按消息类型反射分派。
//
// 与 HandleMessage 的差别**只在没有对应处理器时**:同步请求有调用方在等回包,
// 返回 (nil, nil) 等于一个响应都不发 —— 调用方只能等到超时,而"这条路由不存在"
// 是它可以立刻知道的事。因此这里交由 dispatch 报错,由适配层转成业务失败回给
// 调用方(见 invariants #9)。异步入口相反:没有等待者,不处理是常态。
func (b *BaseActor) HandleCall(msg any) (any, error) { return b.dispatch(msg) }

// dispatch 按消息类型查表分派;没有对应处理器时报错(错误里带具体消息名)。
func (b *BaseActor) dispatch(msg any) (any, error) {
	return b.msgHandler.CallWithMsg(b.Ctx, msg)
}

// AddMsgHandler 为指定对象注册消息分派,返回注册到的方法。
func (b *BaseActor) AddMsgHandler(handler any, prefix ...string) []*gxyutil.MethodMeta {
	return b.msgHandler.AddHandler(handler, prefix...)
}

// ===== 门面内部:由运行时适配对象在生命周期两端调用 =====

// startup 准备运行所需的环境。在业务同步段之前调用。
func (b *BaseActor) startup() {
	// 让本 actor 发起的消息按配置比例开启链路追踪(ADR 0013)。
	// 必须设在进程上:运行时发消息时看的是进程级采样器,节点级只对"节点自身
	// 发起"的消息生效——只设节点级会得到一个永远没有 span 的空追踪。
	if rate := gxytrace.SampleRate(); rate > 0 {
		if err := b.rt.SetTracingSampler(gen.TracingSamplerRatio(rate)); err != nil {
			gxylog.Warn(b.Ctx, "set process tracing sampler failed", gxylog.Err(err))
		}
	}
	gxymetrics.ActorActiveCount.WithLabelValues(b.kind).Inc()
}

// shutdown 收拾运行环境。在业务终止路径之后调用。
func (b *BaseActor) shutdown() {
	if b.timer != nil {
		b.timer.Stop(b.Ctx)
	}
	gxymetrics.ActorActiveCount.WithLabelValues(b.kind).Dec()
}

// stopReason 返回应交给运行时的终止原因;未请求终止时返回 nil。
// 运行时的契约是"回调返回非 nil 即终止",因此 Stop(nil) 需转成正常终止原因。
func (b *BaseActor) stopReason() error {
	if !b.stopRequested {
		return nil
	}
	if b.stopErr != nil {
		return b.stopErr
	}
	return gen.TerminateReasonNormal
}

// prepareOutbound 决定发往某节点的消息是否需要装信封。
// 只有跨节点消息需要;本机投递保持零拷贝。节点未初始化时原样返回。
func (b *BaseActor) prepareOutbound(message any, node string) (any, error) {
	if app == nil || app.node == nil {
		return message, nil
	}
	return app.prepareOutbound(message, node)
}

// sendReply 把异步消息的应答回给发送者;跨节点时自动装信封。
func (b *BaseActor) sendReply(to gen.PID, message any) {
	out, err := b.prepareOutbound(message, string(to.Node))
	if err != nil {
		gxylog.Warn(b.Ctx, "prepare reply failed", gxylog.Err(err))
		return
	}
	if err := b.rt.Send(to, out); err != nil {
		// 应答没送到不该让一个正在服务其它请求的 actor 死掉,只记录。
		gxylog.Warn(b.Ctx, "reply to sender failed", gxylog.Err(err))
	}
}

// sendResponse 把同步请求的结果或业务失败回给调用方。
//
// 必须走响应通道(带上调用方的 ref):用普通发送回包,调用方的 Call 收不到结果
// 会超时,而迟到的应答会被当成一条新消息投递,在调用方触发"无对应处理器"。
func (b *BaseActor) sendResponse(to gen.PID, ref gen.Ref, message any) {
	out, err := b.prepareOutbound(message, string(to.Node))
	if err != nil {
		gxylog.Warn(b.Ctx, "prepare response failed", gxylog.Err(err))
		return
	}
	if err := b.rt.SendResponse(to, ref, out); err != nil {
		gxylog.Warn(b.Ctx, "reply failed", gxylog.Err(err))
	}
}

// ===== 业务常用能力 =====

// Self 返回本 actor 的地址。
func (b *BaseActor) Self() PID { return b.pid }

// Sender 返回当前正在处理的消息的发送者;不在回调中时为零值。
func (b *BaseActor) Sender() PID { return b.sender }

// SendTo 向目标 actor 发送消息;跨节点时自动装信封。
func (b *BaseActor) SendTo(pid PID, message any) error {
	target := pid.target()
	if target == nil {
		return gerror.New("send to empty pid")
	}
	out, err := b.prepareOutbound(message, pid.Node())
	if err != nil {
		return err
	}
	return b.rt.Send(target, out)
}

// Call 同步调用目标 actor;跨节点时自动装信封。
//
// 对端把业务失败作为错误载荷返回(invariants #9),这里还原成 error——调用方
// 才能用统一的 if err != nil 处理。这一步不能省:调用方拿到的是一个"调用成功
// 但内容是失败"的结果,不还原就只能靠类型断言去猜,猜错即 panic。
//
// 与"调用本身失败"区分:超时、对端不存在、对端拒绝调用由运行时直接返回 error。
func (b *BaseActor) Call(pid PID, message any, timeout time.Duration) (any, error) {
	target := pid.target()
	if target == nil {
		return nil, gerror.New("call on empty pid")
	}
	out, err := b.prepareOutbound(message, pid.Node())
	if err != nil {
		return nil, err
	}
	result, err := b.rt.CallWithTimeout(target, out, int(timeout.Seconds()))
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

// Monitor 监视目标 actor;它终止时本 actor 会经 HandleDown 收到通知。
// 注意:该通知早于对端终止回调完成
func (b *BaseActor) Monitor(pid PID) error {
	target := pid.target()
	if target == nil {
		return gerror.New("monitor on empty pid")
	}
	return b.rt.Monitor(target)
}

// Demonitor 取消监视。
func (b *BaseActor) Demonitor(pid PID) error {
	target := pid.target()
	if target == nil {
		return gerror.New("demonitor on empty pid")
	}
	return b.rt.Demonitor(target)
}

func (b *BaseActor) HandleDown(pid PID) {
}

func (b *BaseActor) Terminate(err error) {
}

// SetTracingSpanAttribute 为当前处理的消息附加追踪属性。
// 运行时的追踪自动覆盖消息链路,属性用于补充业务上下文。
func (b *BaseActor) SetTracingSpanAttribute(key, value string) {
	b.rt.SetTracingSpanAttribute(key, value)
}

// Timer 返回本 actor 的定时器。
func (b *BaseActor) Timer() *ActorTimer {
	if b.timer == nil {
		b.timer = newActorTimer(b)
	}
	return b.timer
}

// SetLogValue 为日志上下文附加字段。
func (b *BaseActor) SetLogValue(key string, val any) *BaseActor {
	b.Ctx = gxylog.WithValue(b.Ctx, key, val)
	return b
}

// Stop 请求终止本 actor。终止在本次回调返回后发生;err 为 nil 表示正常终止。
func (b *BaseActor) Stop(err error) {
	b.stopRequested = true
	b.stopErr = err
}

// StopRequested 报告是否已请求终止。
func (b *BaseActor) StopRequested() bool { return b.stopRequested }
