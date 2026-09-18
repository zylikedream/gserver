package gxyactor

import (
	"context"
	"time"

	"gserver/protocol/pb"
)

func RegisterActorKind(name string, prod ActorProducer) error {
	return app.RegisterActorKind(name, prod)
}

func DeregisterActorKind(name string) {
	app.DeregisterActorKind(name)
}

// SpawnFunc 创建一个不带名字的 actor,生命周期由创建者负责。
// 用于会话这类无需跨节点寻址的实例。
func SpawnFunc(prod ActorProducer, initArgs ...any) (PID, error) {
	return app.spawnUnnamed(prod, initArgs...)
}

// Send 发送消息(异步)。
func Send(ctx context.Context, pid PID, message any) error {
	return app.send(ctx, pid, message)
}

// LocalSend 本地发送。运行时中本地投递不经过序列化。
func LocalSend(ctx context.Context, pid PID, message any) error {
	return app.localSend(ctx, pid, message)
}

// Call 同步调用并等待响应,超时返回错误。
func Call(ctx context.Context, pid PID, message any, timeout time.Duration) (any, error) {
	return app.call(ctx, pid, message, timeout)
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
