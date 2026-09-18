package gxyactor

import (
	"context"
	"strconv"

	"github.com/cockroachdb/errors"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxyutil"
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

	kind       string
	biz        any
	msgHandler *gxyutil.MsgHandler
	timer      *ActorTimer

	// currentFrom 是当前正在处理的消息的发送者。
	// 反射分派无法传递发送者,因此由基类在分派前记录,业务用 Sender() 读取。
	currentFrom gen.PID

	// stopErr 记录终止原因;stopRequested 区分"未请求终止"与"正常终止"。
	stopErr       error
	stopRequested bool

	// 本次激活持有的所有权,在同步初始化段获取、终止路径释放。
	owner   ActorOwner
	ownedID string
}

// NewActor 创建基类。biz 是用于消息分派的业务对象,通常是内嵌本基类的结构体自身。
func NewActor(kind string, biz any) *Actor {
	return &Actor{
		Ctx:        gxylog.NewContext(context.Background(), kind),
		kind:       kind,
		biz:        biz,
		msgHandler: gxyutil.NewMsgHandler(),
	}
}

// ActorKind 返回该 actor 的能力名。
func (a *Actor) ActorKind() string {
	return a.kind
}

// ===== 运行时回调:默认实现,业务可覆写并调用基类 =====

// Init 是运行时回调。基类在此获取所有权并注册消息分派。
//
// 所有权必须在同步初始化段获取(见 invariants #3):此时尚未处理任何业务
// 消息,也就不可能带着未确认的归属去服务请求。
func (a *Actor) Init(args ...any) error {
	if id, ok := actorIDFromArgs(args); ok {
		a.ownedID = id
	}
	if a.biz != nil {
		a.msgHandler.AddHandler(a.biz)
	}
	if _, err := a.acquireOwnership(); err != nil {
		return err
	}
	gxymetrics.ActorActiveCount.WithLabelValues(a.kind).Inc()
	return nil
}

// Terminate 是运行时回调。基类在此停定时器并释放所有权。
// 业务覆写时应先完成最终落盘,再调用本方法——顺序不可颠倒(见 invariants #4)。
func (a *Actor) Terminate(reason error) {
	if a.timer != nil {
		a.timer.Stop(a.Ctx)
	}
	gxymetrics.ActorActiveCount.WithLabelValues(a.kind).Dec()
	a.dropOwnership(a.Ctx)
}

// HandleMessage 是运行时回调(异步消息):还原跨节点消息后交给业务分派。
// 定时消息在此分流,不进入业务分派。
func (a *Actor) HandleMessage(from gen.PID, message any) error {
	switch msg := message.(type) {
	case ActorTimerMsg:
		a.timer.Active(a.Ctx, msg)
		return nil
	case gen.MessageCron:
		a.timer.ActiveCron(a.Ctx, msg.Job)
		return nil
	}
	return a.AcceptMessage(from, message)
}

// HandleCall 是运行时回调(同步请求):还原后分派并回包。
func (a *Actor) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	return a.AcceptCall(from, ref, request)
}

// ===== 供业务覆写时调用 =====

// AcceptMessage 还原跨节点消息并交给业务分派。返回非 nil 表示终止本 actor。
func (a *Actor) AcceptMessage(from gen.PID, message any) error {
	a.currentFrom = from
	decoded, err := UnwrapWire(message)
	if err != nil {
		gxylog.Error(a.Ctx, "decode wire message failed", gxylog.Err(err))
		return nil
	}
	rsp, err := a.dispatchTo(decoded)
	gxymetrics.ActorMessages.WithLabelValues(a.kind).Inc()
	if err != nil {
		gxylog.Error(a.Ctx, "handle msg failed", gxylog.Any("payload", decoded), gxylog.Err(err))
		return err
	}
	// 异步消息的响应是处理函数的返回值(如客户端请求的应答):回给发送者。
	// 没有返回值表示该消息不需要应答(如通知类)。
	if rsp != nil && from != (gen.PID{}) {
		if sendErr := a.Reply(from, rsp); sendErr != nil {
			gxylog.Warn(a.Ctx, "reply to sender failed", gxylog.Err(sendErr))
		}
	}
	return a.StopReason()
}

// AcceptCall 还原跨节点消息、分派业务方法,并把结果或业务错误回给调用方。
//
// 业务错误走响应通道而不作终止原因:运行时的契约是"回调返回非 nil error
// 即终止本进程",而业务失败不应终止进程(见 invariants #9)。
func (a *Actor) AcceptCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	a.currentFrom = from
	decoded, err := UnwrapWire(request)
	if err != nil {
		return nil, err
	}
	rsp, err := a.dispatchTo(decoded)
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

// dispatcher 由业务实现以接管消息分派(如加限流、埋点)。
type dispatcher interface {
	Dispatch(message any) (any, error)
}

// dispatchTo 把消息交给分派目标:业务覆写了 Dispatch 就用业务的,
// 否则用按消息类型反射分派的默认实现。
func (a *Actor) dispatchTo(message any) (any, error) {
	if d, ok := a.biz.(dispatcher); ok {
		return d.Dispatch(message)
	}
	return a.DispatchDefault(message)
}

// DispatchDefault 是按消息类型反射分派的默认实现。
// 业务覆写 Dispatch 时应在其中调用本方法,以免递归。
func (a *Actor) DispatchDefault(message any) (any, error) {
	return a.msgHandler.CallWithMsg(a.Ctx, message)
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
	return UnwrapWire(result)
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

// AddMsgHandler 为指定对象注册消息分派,返回注册到的方法。
func (a *Actor) AddMsgHandler(handler any, prefix ...string) []*gxyutil.MethodMeta {
	return a.msgHandler.AddHandler(handler, prefix...)
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

// Owner 返回本次激活持有的所有权,供业务记录(如数据库 fencing)。
func (a *Actor) Owner() ActorOwner {
	return a.owner
}

// ===== 所有权 =====

// claimOwnership / releaseOwnership 是可替换函数变量:测试注入以隔离 Redis
// (编译期安全,非 gomonkey)。
var (
	claimOwnership = func(kind, id string) (ActorOwner, error) {
		if app == nil || app.activator == nil || app.activator.locator == nil {
			return ActorOwner{}, errors.New("actor ownership is not available")
		}
		return app.activator.claim(kind, id)
	}
	releaseOwnership = func(ctx context.Context, kind, id string, owner ActorOwner) error {
		if app == nil || app.activator == nil || app.activator.locator == nil {
			return errors.New("actor ownership is not available")
		}
		_, err := app.activator.release(ctx, kind, id, owner)
		return err
	}
)

func (a *Actor) acquireOwnership() (ActorOwner, error) {
	if a.ownedID == "" {
		return ActorOwner{}, nil
	}
	owner, err := claimOwnership(a.kind, a.ownedID)
	if err != nil {
		return ActorOwner{}, errors.Wrapf(err, "claim ownership for %s/%s", a.kind, a.ownedID)
	}
	a.owner = owner
	return owner, nil
}

// SetOwnershipHooks 替换所有权获取/释放的实现,供测试隔离 Redis。
// 返回值用于恢复原实现。
func SetOwnershipHooks(
	claim func(kind, id string) (ActorOwner, error),
	release func(kind, id string, owner ActorOwner) error,
) func() {
	prevClaim, prevRelease := claimOwnership, releaseOwnership
	claimOwnership = claim
	releaseOwnership = func(_ context.Context, k, i string, o ActorOwner) error {
		return release(k, i, o)
	}
	return func() { claimOwnership, releaseOwnership = prevClaim, prevRelease }
}

func (a *Actor) dropOwnership(ctx context.Context) {
	if a.ownedID == "" || a.owner.NodeID == "" {
		return
	}
	if err := releaseOwnership(ctx, a.kind, a.ownedID, a.owner); err != nil {
		gxylog.Warn(ctx, "release actor ownership failed",
			gxylog.Str("kind", a.kind), gxylog.Str("id", a.ownedID), gxylog.Err(err))
	}
}

// actorIDFromArgs 从初始化参数中取出 actor 标识。
// 标识由 Activator 作为首个参数传入,类型随能力而定(role 用整数,其余用字符串)。
func actorIDFromArgs(args []any) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	switch id := args[0].(type) {
	case string:
		return id, id != ""
	case int64:
		return strconv.FormatInt(id, 10), true
	case int:
		return strconv.Itoa(id), true
	default:
		return "", false
	}
}
