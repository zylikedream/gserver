package gxyactor

import (
	"context"

	"github.com/cockroachdb/errors"

	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
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
	act.Actor

	base *Actor
	biz  Business
}

// ActorFactory 把业务构造器适配成运行时的进程工厂。
//
// 能力的权威名在这里按**注册表的键**写入一次:业务构造器不认识它,也就不存在
// "注册名与能力名不一致"的余地——不一致不会报错,只会让实例注册到查不到的名字
// 下,于是被反复重建。
func ActorFactory(kind string, ctor func() Business) gen.ProcessFactory {
	return func() gen.ProcessBehavior {
		biz := ctor()
		base := biz.actorBase()
		base.kind = kind
		if base.Ctx == nil || base.kind != "" {
			base.Ctx = gxylog.NewContext(context.Background(), kind)
		}
		return newRuntimeActor(base, biz)
	}
}

func newRuntimeActor(base *Actor, biz Business) *runtimeActor {
	return &runtimeActor{base: base, biz: biz}
}

// ===== 运行时回调:只出现在本类型上 =====

// Init 是运行时回调。
//
// 顺序即语义:先备环境,再让业务绑定标识,再取归属(实体层),最后驱动异步段。
// 同步段必须够快且不碰数据库——耗时加载属于异步段。
func (r *runtimeActor) Init(args ...any) error {
	r.base.rt = r
	r.base.self = pidFromLocal(r.PID())
	r.base.initArgs = args
	r.base.startup()

	if s, ok := r.biz.(initializer); ok {
		if err := s.Init(args...); err != nil {
			return err
		}
	}

	// 分派目标是业务对象自身,必须在处理任何消息之前注册。
	// 任何 actor 都可按消息类型分派,与是否承载实体无关。
	r.base.msgHandler.AddHandler(r.biz)

	// 实体层在同步段取归属:此时尚未处理任何业务消息(不变量 #3)。
	if e := entityOf(r.biz); e != nil {
		if _, err := e.acquireOwnership(); err != nil {
			return err
		}
	}

	// 编排归门面:业务只声明异步段做什么,不必自己发消息。
	// 在此投递,邮箱为空,因此它必然先于任何外部业务消息被处理。
	if _, ok := r.biz.(asyncInitializer); ok {
		return r.SendPID(r.PID(), actorInitMsg{})
	}
	return nil
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
		if s, ok := r.biz.(asyncInitializer); ok {
			if err := s.AsyncInit(); err != nil {
				gxylog.Error(r.base.Ctx, "async init failed", gxylog.Err(err))
				return err
			}
		}
		return nil
	case ActorTimerMsg:
		r.base.timer.Active(r.base.Ctx, msg)
		return nil
	case gen.MessageCron:
		r.base.timer.ActiveCron(r.base.Ctx, msg.Job)
		return nil
	case *pb.ActorStop:
		// 按消息请求停止:调用方(角色、网关)用它结束会话。
		// 会话在本分支不承载实体,停止只是终止自身。
		r.base.Stop(errors.New(msg.Reason))
		return r.base.stopReason()
	case gen.MessageDownPID:
		// 监视通知翻译成门面身份后交给业务:业务侧不出现运行时消息类型。
		if h, ok := r.biz.(downHandler); ok {
			h.HandleDown(PidFromRuntime(msg.PID))
		}
		return r.base.stopReason()
	}

	r.base.sender = pidFromLocal(from)
	decoded, err := UnwrapWire(message)
	if err != nil {
		// 报文不合法不足以让 actor 死掉:丢掉这一条,继续服务。
		gxylog.Error(r.base.Ctx, "decode wire message failed", gxylog.Err(err))
		return nil
	}

	// 断言必然成立:业务对象经内嵌基类获得默认入口(见 Actor.HandleMessage)。
	rsp, err := r.biz.(messageHandler).HandleMessage(decoded)
	gxymetrics.ActorMessages.WithLabelValues(r.base.kind).Inc()
	if err != nil {
		gxylog.Error(r.base.Ctx, "handle msg failed", gxylog.Any("payload", decoded), gxylog.Err(err))
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
	r.base.sender = pidFromLocal(from)
	decoded, err := UnwrapWire(request)
	if err != nil {
		return nil, err
	}

	// 断言必然成立:业务对象经内嵌基类获得默认入口(见 Actor.HandleCall)。
	rsp, err := r.biz.(callHandler).HandleCall(decoded)
	gxymetrics.ActorMessages.WithLabelValues(r.base.kind).Inc()
	if err != nil {
		gxylog.Error(r.base.Ctx, "handle rpc msg failed", gxylog.Any("payload", decoded), gxylog.Err(err))
		r.base.sendResponse(from, ref, &pb.ActorError{Reason: err.Error()})
	} else if rsp != nil {
		r.base.sendResponse(from, ref, rsp)
	}
	// (nil, nil) 表示已自行回复。
	return nil, r.base.stopReason()
}

// Terminate 是运行时回调。
//
// 顺序即语义:先让业务完成最终落盘,再由实体层释放归属(不变量 #4),最后收拾
// 运行环境。颠倒会让新持有者推进世代,使本次落盘被拒。
func (r *runtimeActor) Terminate(reason error) {
	if t, ok := r.biz.(terminator); ok {
		t.Terminate(reason)
	}
	if e := entityOf(r.biz); e != nil {
		e.dropOwnership(r.base.Ctx)
	}
	r.base.shutdown()
	r.base.rt = nil
}
