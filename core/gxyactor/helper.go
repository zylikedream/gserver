package gxyactor

import (
	"gserver/protocol/pb"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"google.golang.org/protobuf/proto"
)

func RegisterGrainProducer(name string, prod GrainProducer) error {
	return app.RegisterGrainProducer(name, prod)
}

func DeRegisterGrain(name string) {
	app.DeRegisterGrain(name)
}

// SpawnRegister创建新的Actor
func SpawnNamed(props *actor.Props, name string) (PID, error) {
	return app.spawnNamed(props, name)
}

func SpawnNamedFunc(name string, prod func() actor.Actor) (PID, error) {
	props := actor.PropsFromProducer(prod, actor.WithSupervisor(newSupervisor()))
	return app.spawnNamed(props, name)
}

func Spawn(props *actor.Props) (pid PID, err error) {
	return app.spawn(props)
}

func SpawnFunc(prod func() actor.Actor) (pid PID, err error) {
	props := actor.PropsFromProducer(prod, actor.WithSupervisor(newSupervisor()))
	return app.spawn(props)
}

// Send 发送消息（异步）
func Send(pid PID, message proto.Message) error {
	return app.send(pid, message)
}

func LocalSend(pid PID, message any) {
	app.localSend(pid, message)
}

func Call(pid PID, message proto.Message, timeout time.Duration) (any, error) {
	return app.call(pid, message, timeout)
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

func GetGrain(kind string, id string, spawn ...bool) (PID, error) {
	return app.GetGrain(kind, id, spawn...)
}

func GetGrainCount(kind string) int {
	return app.GetGrainCount(kind)
}

func ActorError(reason string) *pb.ActorError {
	return &pb.ActorError{
		Reason: reason,
	}
}
