package gxyactor

import (
	"github.com/asynkron/protoactor-go/actor"
	"google.golang.org/protobuf/proto"
)

func RegisterGrain(name string, prod GrainProducer) error {
	return app.RegisterGrain(name, prod)
}

func DeRegisterGrain(name string) {
	app.DeRegisterGrain(name)
}

// SpawnRegister创建新的Actor
func SpawnNamed(name string, prod func() actor.Actor) (PID, error) {
	return app.SpawnNamed(name, prod)
}

func Spawn(prod func() actor.Actor) (pid PID, err error) {
	return app.Spawn(prod)
}

// Send 发送消息（异步）
func Send(pid PID, message any) error {
	return app.Send(pid, message)
}

func Call(pid PID, message any) (any, error) {
	return app.Call(pid, message)
}

func RpcCall(pid PID, message proto.Message) (proto.Message, error) {
	return app.RpcCall(pid, message)
}

func Notify(pid PID, message any) error {
	return app.Notify(pid, message)
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
