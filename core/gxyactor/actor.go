package gxyactor

import (
	"context"

	"github.com/cockroachdb/errors"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxytrace"
	"gserver/protocol/pb"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/errors/gerror"
)

// ActorProducer 生产业务 actor 的行为实现。
// 直接返回运行时的行为接口——门面不再做一层包装,业务结构体内嵌 *Actor 即满足。
type ActorProducer func() act.ActorBehavior

// asFactory 把业务生产者适配成运行时的进程工厂,并把**注册名**交给实例。
//
// 注册式 actor 的能力名必须与注册表的键一致:所有权键按能力名区分,
// 与查询方使用的键不一致会让同一实体的第二次激活查不到归属而重复创建。
// 因此能力名由框架在此处一次性写入,实例不再自行推断。
func asFactory(kind string, prod ActorProducer) gen.ProcessFactory {
	return func() gen.ProcessBehavior {
		behavior := prod()
		if kind != "" {
			if a, ok := behavior.(*Actor); ok {
				a.kind = kind
			}
		}
		return behavior
	}
}

// ActorInitMsg 是业务在初始化期间自投的消息。
//
// 用途:把耗时的加载放到初始化返回之后。初始化期间队列为空,因此本消息
// 必然先于任何外部业务消息被处理。
type ActorInitMsg struct{}

// ActorTimerMsg 定时器到期的内部消息,只携带定时名。
// 触发时刻由接收方在执行时确定,不需要跨进程传递。
type ActorTimerMsg struct {
	Name string
}

// Actor 是业务 actor 的基类,内嵌运行时的 actor 行为。
//
// 业务只需覆写自己关心的回调,其余由运行时与基类提供。基类补的是运行时没给
// 而项目需要的东西:日志上下文(运行时回调不传 context)、按消息类型的反射
// 分派、定时器、以及注册式 actor 的所有权。
//
// 业务覆写回调时的约定——显式调用基类同名方法:
//
//	func (g *MyActor) Init(args ...any) error {
//	    if err := g.Actor.Init(args...); err != nil { return err }  // 获取所有权 + 注册分派
//	    return g.loadFromDB(g.Ctx)                                   // 自己的初始化
//	}
//
//	func (g *MyActor) Terminate(reason error) {
//	    g.save(g.Ctx)              // 先落盘
//	    g.Actor.Terminate(reason)  // 再停定时器、释放所有权(顺序不可颠倒)
//	}
type Actor struct {
	act.Actor

	// Ctx 是带日志与追踪上下文的 context。
	Ctx context.Context

	kind string
	// biz 是外层业务对象(通常是内嵌本基类的结构体自身)。
	// 必须显式持有:Go 的内嵌是静态分派,*Actor 内调用 a.Receive 只会调到
	// 基类自己的实现,不会调到外层的覆写。经此引用才能到达业务入口。
	biz   any
	timer *ActorTimer

	// currentFrom 是当前正在处理的消息的发送者。
	// 反射分派无法传递发送者,因此由基类在分派前记录,业务用 Sender() 读取。
	currentFrom gen.PID

	// stopErr 记录终止原因;stopRequested 区分"未请求终止"与"正常终止"。
	stopErr       error
	stopRequested bool
}

// NewActor 创建接入运行时的基类。承载持久实体的 actor 应内嵌 EntityActor。
// biz 是外层业务对象(通常是内嵌本基类的结构体自身),用于到达业务入口。
func NewActor(kind string, biz any) *Actor {
	return &Actor{
		Ctx:  gxylog.NewContext(context.Background(), kind),
		kind: kind,
		biz:  biz,
	}
}

// ActorKind 返回该 actor 的能力名。
func (a *Actor) ActorKind() string {
	return a.kind
}

// ===== 运行时回调:默认实现,业务可覆写并调用基类 =====

// Init 是运行时回调。基类在此准备运行所需的环境。
//
// 承载持久实体的 actor 由 EntityActor 覆写本方法,先取得归属再调回这里。
func (a *Actor) Init(_ ...any) error {
	// 让本 actor 发起的消息按配置比例开启链路追踪(ADR 0013)。
	// 必须设在进程上:运行时发消息时看的是进程级采样器,节点级只对"节点自身
	// 发起"的消息生效——只设节点级会得到一个永远没有 span 的空追踪。
	if rate := gxytrace.SampleRate(); rate > 0 {
		if err := a.SetTracingSampler(gen.TracingSamplerRatio(rate)); err != nil {
			gxylog.Warn(a.Ctx, "set process tracing sampler failed", gxylog.Err(err))
		}
	}
	gxymetrics.ActorActiveCount.WithLabelValues(a.kind).Inc()
	return nil
}

// Terminate 是运行时回调。基类在此停定时器。
// 承载持久实体的 actor 由 EntityActor 覆写本方法,释放归属在本方法之后。
func (a *Actor) Terminate(reason error) {
	if a.timer != nil {
		a.timer.Stop(a.Ctx)
	}
	gxymetrics.ActorActiveCount.WithLabelValues(a.kind).Dec()
}

// HandleMessage 是运行时回调(异步消息)。**业务不覆写本方法**——见 ADR 0016。
//
// 门面在此承担与业务无关的机制:定时消息分流(触发经邮箱投递,必须在这里回到
// 注册的回调)、跨节点消息还原。业务处理从 Receive 进入;若两件事共用一个名字,
// 覆写业务处理就会连机制一起关掉,而失效是静默的。
func (a *Actor) HandleMessage(from gen.PID, message any) error {
	switch msg := message.(type) {
	case ActorTimerMsg:
		a.timer.Active(a.Ctx, msg)
		return nil
	case gen.MessageCron:
		a.timer.ActiveCron(a.Ctx, msg.Job)
		return nil
	}
	return a.acceptMessage(from, message)
}

// HandleCall 是运行时回调(同步请求)。**业务不覆写本方法**——见 ADR 0016。
func (a *Actor) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	return a.acceptCall(from, ref, request)
}

// messageReceiver / callReceiver 是业务入口的形态。业务按需实现其一或两者;
// 未实现表示该 actor 不处理这类消息。
type messageReceiver interface {
	Receive(from gen.PID, message any) (any, error)
}

type callReceiver interface {
	ReceiveCall(from gen.PID, ref gen.Ref, request any) (any, error)
}

// receive 把消息交给业务入口。未实现业务入口的 actor 不处理它。
func (a *Actor) receive(from gen.PID, message any) (any, error) {
	if r, ok := a.biz.(messageReceiver); ok {
		return r.Receive(from, message)
	}
	return nil, nil
}

// receiveCall 把请求交给业务入口。未实现业务入口的 actor 不处理它。
func (a *Actor) receiveCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	if r, ok := a.biz.(callReceiver); ok {
		return r.ReceiveCall(from, ref, request)
	}
	return nil, nil
}

// acceptMessage 还原跨节点消息,交给业务入口,并把应答回给发送者。
func (a *Actor) acceptMessage(from gen.PID, message any) error {
	a.currentFrom = from
	decoded, err := UnwrapWire(message)
	if err != nil {
		gxylog.Error(a.Ctx, "decode wire message failed", gxylog.Err(err))
		return nil
	}
	rsp, err := a.receive(from, decoded)
	gxymetrics.ActorMessages.WithLabelValues(a.kind).Inc()
	if err != nil {
		gxylog.Error(a.Ctx, "handle msg failed", gxylog.Any("payload", decoded), gxylog.Err(err))
		return err
	}
	// 异步消息的应答是处理函数的返回值;没有返回值表示不需要应答。
	if rsp != nil && from != (gen.PID{}) {
		if sendErr := a.Reply(from, rsp); sendErr != nil {
			gxylog.Warn(a.Ctx, "reply to sender failed", gxylog.Err(sendErr))
		}
	}
	return a.StopReason()
}

// acceptCall 还原跨节点消息,交给业务入口,并把结果或业务错误回给调用方。
//
// 业务错误走响应通道而不作终止原因:运行时的契约是"回调返回非 nil error
// 即终止本进程",而业务失败不应终止进程(见 invariants #9)。
func (a *Actor) acceptCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	a.currentFrom = from
	decoded, err := UnwrapWire(request)
	if err != nil {
		return nil, err
	}
	rsp, err := a.receiveCall(from, ref, decoded)
	gxymetrics.ActorMessages.WithLabelValues(a.kind).Inc()
	// 同步请求的应答必须走响应通道(带上调用方的 ref):用普通发送回包,
	// 调用方的 Call 收不到结果会超时,而迟到的应答会被当成一条新消息投递,
	// 在调用方触发"无对应处理器"。
	if err != nil {
		gxylog.Error(a.Ctx, "handle rpc msg failed", gxylog.Any("payload", decoded), gxylog.Err(err))
		if sendErr := a.ReplyCall(from, ref, &pb.ActorError{Reason: err.Error()}); sendErr != nil {
			gxylog.Warn(a.Ctx, "reply business error failed", gxylog.Err(sendErr))
		}
	} else if rsp != nil {
		if sendErr := a.ReplyCall(from, ref, rsp); sendErr != nil {
			gxylog.Warn(a.Ctx, "reply failed", gxylog.Err(sendErr))
		}
	}
	// (nil, nil) 表示已自行回复。
	return nil, a.StopReason()
}

// ===== 业务常用能力 =====

// Watch 监视目标 actor;它终止时本进程会收到 gen.MessageDownPID。
// 注意:该通知早于对端终止回调完成,不得作为"对端已落盘"的依据。
func (a *Actor) Watch(pid PID) error {
	target := pid.target()
	if target == nil {
		return gerror.New("watch on empty pid")
	}
	return a.Monitor(target)
}

// Unwatch 取消监视。
func (a *Actor) Unwatch(pid PID) error {
	target := pid.target()
	if target == nil {
		return gerror.New("unwatch on empty pid")
	}
	return a.Demonitor(target)
}

// SendTo 向目标 actor 发送消息;跨节点时自动装信封。
func (a *Actor) SendTo(pid PID, message any) error {
	target := pid.target()
	if target == nil {
		return gerror.New("send to empty pid")
	}
	out, err := a.PrepareOutbound(message, pid.Node())
	if err != nil {
		return err
	}
	return a.Send(target, out)
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
	out, err := a.PrepareOutbound(message, pid.Node())
	if err != nil {
		return nil, err
	}
	result, err := a.CallWithTimeout(target, out, int(timeout.Seconds()))
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

// SendSelfInit 自投初始化消息以驱动异步加载。
// 初始化期间邮箱为空,因此本消息必然先于任何外部业务消息被处理。
func (a *Actor) SendSelfInit() error {
	if err := a.SendPID(a.PID(), &ActorInitMsg{}); err != nil {
		return gerror.Wrap(err, "send init message")
	}
	return nil
}

// Sender 返回当前正在处理的消息的发送者;不在回调中时为零值。
func (a *Actor) Sender() PID {
	return PID{local: a.currentFrom}
}

// Self 返回本 actor 的地址。
func (a *Actor) Self() PID {
	return PID{local: a.PID()}
}

// SetTracingSpanAttribute 为当前处理的消息附加追踪属性。
// 运行时的追踪自动覆盖消息链路,属性用于补充业务上下文。
func (a *Actor) SetTracingSpanAttribute(key, value string) {
	a.Actor.SetTracingSpanAttribute(key, value)
}

// Stop 请求终止本 actor。终止在本次回调返回后发生;err 为 nil 表示正常终止。
func (a *Actor) Stop(err error) {
	a.stopRequested = true
	a.stopErr = err
}

// StopRequested 报告是否已请求终止。
func (a *Actor) StopRequested() bool {
	return a.stopRequested
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

// Reply 向发送者回包。跨节点时自动装信封。
func (a *Actor) Reply(from gen.PID, message any) error {
	out, err := a.PrepareOutbound(message, string(from.Node))
	if err != nil {
		return err
	}
	return a.SendPID(from, out)
}

// ReplyError 以业务错误的形式回包。
func (a *Actor) ReplyError(from gen.PID, err error) error {
	return a.Reply(from, &pb.ActorError{Reason: err.Error()})
}

// ReplyCall 回复同步请求;跨节点时自动装信封。
func (a *Actor) ReplyCall(from gen.PID, ref gen.Ref, message any) error {
	out, err := a.PrepareOutbound(message, string(from.Node))
	if err != nil {
		return err
	}
	return a.SendResponse(from, ref, out)
}

// StopReason 返回应交给运行时的终止原因;未请求终止时返回 nil。
// 运行时的契约是"返回非 nil 即终止",因此 Stop(nil) 需转成正常终止原因。
func (a *Actor) StopReason() error {
	if !a.stopRequested {
		return nil
	}
	if a.stopErr != nil {
		return a.stopErr
	}
	return gen.TerminateReasonNormal
}

// PrepareOutbound 决定发往某节点的消息是否需要装信封。
// 只有跨节点消息需要;本机投递保持零拷贝。节点未初始化时原样返回。
func (a *Actor) PrepareOutbound(message any, node string) (any, error) {
	if app == nil || app.node == nil {
		return message, nil
	}
	return app.prepareOutbound(message, node)
}
