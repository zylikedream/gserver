package gxyactor

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"
	"gserver/protocol/pb"
)

func runtimeOrError() (Runtime, error) { return currentRuntime() }

func RegisterActorKind(name string, prod ActorProducer) error {
	runtime, err := runtimeOrError()
	if err != nil {
		return err
	}
	return runtime.RegisterActorKind(name, prod)
}

func DeregisterActorKind(name string) {
	if runtime, err := runtimeOrError(); err == nil {
		runtime.DeregisterActorKind(name)
	}
}

// SpawnFunc is the public anonymous-spawn helper; runtime-specific process details stay in the adapter.
func SpawnFunc(prod ActorProducer, initArgs ...any) (PID, error) {
	runtime, err := runtimeOrError()
	if err != nil {
		return PID{}, err
	}
	spawner, ok := runtime.(interface {
		SpawnAnonymous(ActorProducer, ...any) (PID, error)
	})
	if !ok {
		return PID{}, errors.New("actor runtime does not support anonymous spawn")
	}
	return spawner.SpawnAnonymous(prod, initArgs...)
}

func Send(ctx context.Context, pid PID, message any) error {
	if runtime := runtimeFromContext(ctx); runtime != nil {
		return runtime.Send(ctx, pid, message)
	}
	runtime, err := runtimeOrError()
	if err != nil {
		return err
	}
	return runtime.Send(ctx, pid, message)
}
func LocalSend(ctx context.Context, pid PID, message any) error {
	if runtime := runtimeFromContext(ctx); runtime != nil {
		return runtime.LocalSend(ctx, pid, message)
	}
	runtime, err := runtimeOrError()
	if err != nil {
		return err
	}
	return runtime.LocalSend(ctx, pid, message)
}
func Respond(ctx context.Context, request Request, message any, responseErr ...error) error {
	err := error(nil)
	if len(responseErr) > 0 {
		err = responseErr[0]
	}
	if bound, ok := request.(interface{ Runtime() Runtime }); ok {
		if runtime := bound.Runtime(); runtime != nil {
			return runtime.Respond(ctx, request, message, err)
		}
	}
	if runtime := runtimeFromContext(ctx); runtime != nil {
		return runtime.Respond(ctx, request, message, err)
	}
	runtime, runtimeErr := runtimeOrError()
	if runtimeErr != nil {
		return runtimeErr
	}
	return runtime.Respond(ctx, request, message, err)
}
func Call(ctx context.Context, pid PID, message any, timeout time.Duration) (any, error) {
	if runtime := runtimeFromContext(ctx); runtime != nil {
		return runtime.Call(ctx, pid, message, timeout)
	}
	runtime, err := runtimeOrError()
	if err != nil {
		return nil, err
	}
	return runtime.Call(ctx, pid, message, timeout)
}
func CallSync(ctx context.Context, pid PID, message any, sender PID) error {
	runtime, err := runtimeOrError()
	if err != nil {
		return err
	}
	syncer, ok := runtime.(interface {
		CallSync(context.Context, PID, any, PID) error
	})
	if !ok {
		return errors.New("actor runtime does not support CallSync")
	}
	return syncer.CallSync(ctx, pid, message, sender)
}
func GetNodeName() string {
	runtime, err := runtimeOrError()
	if err != nil {
		return ""
	}
	if named, ok := runtime.(interface{ NodeName() string }); ok {
		return named.NodeName()
	}
	return ""
}
func StopActor(pid PID) error {
	runtime, err := runtimeOrError()
	if err != nil {
		return err
	}
	return runtime.Stop(pid)
}
func Stop(pid PID) error { return StopActor(pid) }
func Host() string {
	runtime, err := runtimeOrError()
	if err != nil {
		return ""
	}
	if v, ok := runtime.(interface{ Host() string }); ok {
		return v.Host()
	}
	return ""
}
func Address() string {
	runtime, err := runtimeOrError()
	if err != nil {
		return ""
	}
	if v, ok := runtime.(interface{ Address() string }); ok {
		return v.Address()
	}
	return ""
}
func ActivateActor(ctx context.Context, kind, id string, spawn bool) (PID, error) {
	runtime, err := runtimeOrError()
	if err != nil {
		return PID{}, err
	}
	return runtime.ActivateActor(ctx, kind, id, spawn)
}
func GetActorOwner(ctx context.Context, kind, id string) (ActorOwner, error) {
	runtime, err := runtimeOrError()
	if err != nil {
		return ActorOwner{}, err
	}
	if v, ok := runtime.(interface {
		GetActorOwner(context.Context, string, string) (ActorOwner, error)
	}); ok {
		return v.GetActorOwner(ctx, kind, id)
	}
	return ActorOwner{}, errors.New("actor owner lookup is unavailable")
}
func GetActorCount(kind string) int {
	runtime, err := runtimeOrError()
	if err != nil {
		return 0
	}
	return len(runtime.GetLocalActorAll(kind))
}
func GetLocalActor(kind, id string) PID {
	runtime, err := runtimeOrError()
	if err != nil {
		return PID{}
	}
	return runtime.GetLocalActor(kind, id)
}
func GetLocalActorAll(kind string) []PID {
	runtime, err := runtimeOrError()
	if err != nil {
		return nil
	}
	return runtime.GetLocalActorAll(kind)
}
func NodeInstanceName() string {
	runtime, err := runtimeOrError()
	if err != nil {
		return ""
	}
	if v, ok := runtime.(interface{ NodeInstanceName() string }); ok {
		return v.NodeInstanceName()
	}
	return ""
}
func ActorError(reason string) *pb.ActorError { return &pb.ActorError{Reason: reason} }
