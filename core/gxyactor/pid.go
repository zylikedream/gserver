package gxyactor

import (
	"gserver/protocol/pb"

	"ergo.services/ergo/gen"
)

// PID 是 actor 的地址,用于发送消息与判断身份。
//
// 运行时把"本机进程"和"跨节点进程"分成两种标识,业务不需要知道这个区别,
// 因此门面统一成一种地址:
//
//   - 本机:按运行时进程标识寻址。
//   - 跨节点:按"节点名 + 注册名"寻址。运行时的进程标识含"哪一代实例"
//     的信息,跨节点投递会校验它,对端重启后旧标识即失效;注册名是稳定的
//     逻辑身份,对端重启后同一逻辑 actor 仍可收到消息。
//
// 因此跨节点比较只比到节点与名字,比不出"是不是同一个进程实例"——对业务
// 关心的逻辑身份而言这已足够。
type PID struct {
	local  gen.PID       // 本机引用,零值表示无本机引用
	remote gen.ProcessID // 跨节点引用,零值表示非同节点引用
}

// IsZero 报告该地址是否为空。
func (p PID) IsZero() bool {
	return p.local.Node == "" && p.remote.Node == ""
}

// Node 返回所属节点名。
func (p PID) Node() string {
	if p.local.Node != "" {
		return string(p.local.Node)
	}
	return string(p.remote.Node)
}

// Name 返回注册名;本机引用未注册名字时为空。
func (p PID) Name() string {
	if p.remote.Name != "" {
		return string(p.remote.Name)
	}
	if p.local.Node != "" && app != nil && app.node != nil {
		return pidName(p.local)
	}
	return ""
}

// String 返回可读表示,用于日志。
func (p PID) String() string {
	switch {
	case !p.IsZero() && p.local.Node != "":
		return p.local.String()
	case p.remote.Node != "":
		return "<" + string(p.remote.Node) + "." + string(p.remote.Name) + ">"
	default:
		return "<zero>"
	}
}

// target 返回运行时可寻址的目标。本机优先用进程标识,跨节点用注册名。
// 零值返回 nil。
func (p PID) target() any {
	if p.local.Node != "" {
		return p.local
	}
	if p.remote.Node != "" {
		return p.remote
	}
	return nil
}

// PIDIsZero 判断地址是否为空。
func PIDIsZero(pid PID) bool {
	return pid.IsZero()
}

// pidFromLocal 由运行时进程标识构造引用。
func pidFromLocal(p gen.PID) PID {
	return PID{local: p}
}

// PidFromRuntime 由运行时进程标识构造引用。
// 供业务处理运行时回调参数(如监视通知)时使用。
func PidFromRuntime(p gen.PID) PID {
	return pidFromLocal(p)
}

// NewPID 按节点与名字构造引用,供测试与外部调用方使用。
// 名字以字符串给出是刻意的:外部调用方手里就是字符串(配置、字面量、proto 字段),
// 在这里转换一次,内部接缝即可全程用运行时类型。
func NewPID(node string, name string) PID {
	return pidFromRemote(node, gen.Atom(name))
}

// pidFromRemote 由节点与注册名构造跨节点引用。
// 名字用运行时表示,与身份派生出的注册名类型一致——内部调用方不必来回转换。
func pidFromRemote(node string, name gen.Atom) PID {
	return PID{remote: gen.ProcessID{Node: gen.Atom(node), Name: name}}
}

// PidEqual 比较两个引用是否为同一身份。
//
// 本机引用比完整运行时标识(节点、序号、创建时刻)——同节点重启产生的新实例
// 不会被误判为同一个。跨节点引用只能比到节点与注册名。两者混比时退化到节点
// 加名字。
func PidEqual(a, b PID) bool {
	if a.IsZero() || b.IsZero() {
		return a.IsZero() && b.IsZero()
	}
	if a.local.Node != "" && b.local.Node != "" {
		return a.local == b.local
	}
	return a.Node() == b.Node() && a.Name() == b.Name()
}

// PidToPB 把进程引用转成可跨节点传递的表示。
//
// Name 装的是**注册名**(形如 <kind>/<id>),不是实例标识;跨节点只能按它寻址,
// 因为运行时的进程标识含"哪一代实例"的信息,对端重启后即失效。
func PidToPB(pid PID) *pb.ActorPid {
	return &pb.ActorPid{
		Address:  pid.Node(),
		Name:     pid.Name(),
		Pid:      pid.local.ID,
		Creation: pid.local.Creation,
	}
}

// PBToPid 从 wire 表示还原进程引用。
//
// 按**是否带本机进程标识**分两种:带了就按它寻址(同节点、同代);没带就按
// "节点 + 注册名"寻址(跨节点)。判据取 `Node` 与 `Creation` 都非零——两者是
// 本机标识的组成部分,零值即表示这是一个按名寻址的引用。不单看 `Creation`:
// 那会让判别依赖"创建时刻恰好非零"这一实现细节。
func PBToPid(in *pb.ActorPid) PID {
	if in == nil {
		return PID{}
	}
	if in.GetAddress() != "" && in.GetPid() != 0 && in.GetCreation() != 0 {
		return PID{local: gen.PID{
			Node:     gen.Atom(in.GetAddress()),
			ID:       in.GetPid(),
			Creation: in.GetCreation(),
		}}
	}
	if in.GetAddress() == "" || in.GetName() == "" {
		return PID{}
	}
	return pidFromRemote(in.GetAddress(), gen.Atom(in.GetName()))
}

// pidName 反查进程的注册名。名字表是单向的(名字→进程),因此需要遍历查找;
// 仅用于构造跨节点引用与日志,不在热路径上。
func pidName(pid gen.PID) string {
	if app == nil || app.node == nil || pid.Node == "" {
		return ""
	}
	var name string
	_ = app.node.ProcessRangeShortInfo(func(info gen.ProcessShortInfo) bool {
		if info.PID == pid && info.Name != "" {
			name = string(info.Name)
			return false
		}
		return true
	})
	return name
}
