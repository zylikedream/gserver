package gxyactor

import (
	"context"

	"gserver/protocol/pb"
)

func RegisterActorKind(name string, ctor ActorConstructor) error {
	return app.RegisterActorKind(name, ctor)
}

func DeregisterActorKind(name string) {
	app.DeregisterActorKind(name)
}

// SpawnFunc 创建一个不带名字的 actor,生命周期由创建者负责。
// 用于会话这类无需跨节点寻址的实例。
//
// kind 在这里给出一次即可:无名实例不参与按名寻址,没有注册表的键可作为权威
// 能力名,因此构造处就是唯一来源(见 ADR 0017)。
func SpawnFunc(kind string, ctor ActorConstructor, initArgs ...any) (PID, error) {
	return app.spawnUnnamed(kind, ctor, initArgs...)
}

// SendAsNode 以**节点身份**发送消息(发送者是节点本身,不是某个 actor)。
//
// 只给没有进程身份的调用方用:网络回调、生命周期钩子等。actor 内部请用
// Actor.SendTo / Actor.Call —— 用本函数会让接收方把发送者记成节点,回包
// 发到节点上并丢失。名字里的 AsNode 就是提醒这一点。
func SendAsNode(ctx context.Context, pid PID, message any) error {
	return app.send(ctx, pid, message)
}

// ActivateActor 解析或创建 actor,返回可寻址的引用。
// spawn=false 时只查询,不创建。
func ActivateActor(ctx context.Context, kind string, id string, spawn bool) (PID, error) {
	return app.ActivateActor(ctx, kind, id, spawn)
}

// GetActorOwner 返回 actor 的当前归属节点。
func GetActorOwner(ctx context.Context, kind string, id string) (ActorOwner, error) {
	return app.GetActorOwner(ctx, kind, id)
}

// GetActorCount 返回本节点上该 kind 的实例数。
func GetActorCount(kind string) int {
	return app.GetActorCount(kind)
}

// GetLocalActor 返回本节点上该实例的引用;不存在时为零值。
func GetLocalActor(kind string, id string) PID {
	return app.GetLocalActor(kind, id)
}

// GetLocalActorAll 返回本节点上该 kind 的全部实例。
func GetLocalActorAll(kind string) []PID {
	return app.GetLocalActorAll(kind)
}

// ActorError 构造业务错误响应。
func ActorError(reason string) *pb.ActorError {
	return &pb.ActorError{
		Reason: reason,
	}
}
