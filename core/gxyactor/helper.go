package gxyactor

import (
	"context"
	"time"

	"gserver/protocol/pb"

	"github.com/asynkron/protoactor-go/actor"
	"google.golang.org/protobuf/proto"
)

func RegisterActorKind(name string, prod ActorProducer) error {
	return app.RegisterActorKind(name, prod)
}

func DeregisterActorKind(name string) {
	app.DeregisterActorKind(name)
}

// SpawnNamed 创建具名Actor，initArgs 通过 ContextDecorator 传递给 Actor 的 Init
func SpawnNamed(props *actor.Props, name string, initArgs ...any) (PID, error) {
	return app.spawnNamed(props, name, initArgs...)
}

func SpawnNamedFunc(name string, prod func() actor.Actor, initArgs ...any) (PID, error) {
	props := actor.PropsFromProducer(prod, actor.WithSupervisor(newSupervisor()))
	return app.spawnNamed(props, name, initArgs...)
}

func Spawn(props *actor.Props, initArgs ...any) (pid PID, err error) {
	return app.spawn(props, initArgs...)
}

func SpawnFunc(prod func() actor.Actor, initArgs ...any) (pid PID, err error) {
	props := actor.PropsFromProducer(prod, actor.WithSupervisor(newSupervisor()))
	return app.spawn(props, initArgs...)
}

// Send 发送消息（异步）
func Send(ctx context.Context, pid PID, message proto.Message) error {
	return app.send(ctx, pid, message)
}

func LocalSend(ctx context.Context, pid PID, message any) error {
	return app.localSend(ctx, pid, message)
}

func Respond(ctx context.Context, actx actor.Context, message any) error {
	return app.respond(ctx, actx, message)
}

func Call(ctx context.Context, pid PID, message proto.Message, timeout time.Duration) (any, error) {
	return app.call(ctx, pid, message, timeout)
}

func CallSync(ctx context.Context, pid PID, message proto.Message, sender PID) {
	app.callSync(ctx, pid, message, sender)
}

func GetNodeName() string {
	return app.GetNodeName()
}

func StopActor(pid PID) error {
	return app.StopActor(pid)
}

func Host() string {
	return app.Host()
}

func Address() string {
	return app.Address()
}

func ActivateActor(ctx context.Context, kind string, id string, spawn bool) (PID, error) {
	return app.ActivateActor(ctx, kind, id, spawn)
}

func GetActorCount(kind string) int {
	return app.GetActorCount(kind)
}

func GetLocalActor(kind string, id string) PID {
	return app.GetLocalActor(kind, id)
}

func GetLocalActorAll(kind string) []PID {
	return app.GetLocalActorAll(kind)
}

func ActorError(reason string) *pb.ActorError {
	return &pb.ActorError{
		Reason: reason,
	}
}
