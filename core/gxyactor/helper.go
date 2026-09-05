package gxyactor

import (
	"context"
	"time"

	"gserver/protocol/pb"

	"github.com/cockroachdb/errors"
)

func RegisterActorKind(name string, prod ActorProducer) error {
	if runtime, err := currentRuntime(); err == nil { return runtime.RegisterActorKind(name, prod) }
	if app == nil { return errors.New("actor app is not initialized") }
	return app.RegisterActorKind(name, prod)
}

func DeregisterActorKind(name string) {
	if app != nil { app.DeregisterActorKind(name) }
}

// Spawn helpers accept an opaque runtime-specific producer/property value for
// compatibility during the adapter cutover. Business code should register an
// ActorProducer instead; the value is consumed only by the private adapter.
func SpawnNamed(props any, name string, initArgs ...any) (PID, error) {
	if app == nil { return PID{}, errors.New("actor app is not initialized") }
	return app.spawnNamed(props, name, initArgs...)
}

func SpawnNamedFunc(prod any, initArgs ...any) (PID, error) {
	if app == nil { return PID{}, errors.New("actor app is not initialized") }
	return app.spawnFunc(prod, initArgs...)
}

func Spawn(props any, initArgs ...any) (PID, error) {
	if app == nil { return PID{}, errors.New("actor app is not initialized") }
	return app.spawn(props, initArgs...)
}

func SpawnFunc(prod any, initArgs ...any) (PID, error) {
	if app == nil { return PID{}, errors.New("actor app is not initialized") }
	return app.spawnFunc(prod, initArgs...)
}

func Send(ctx context.Context, pid PID, message any) error {
	if runtime, err := currentRuntime(); err == nil { return runtime.Send(ctx, pid, message) }
	if app == nil { return errors.New("actor app is not initialized") }
	return app.send(ctx, pid, message)
}

func LocalSend(ctx context.Context, pid PID, message any) error { return Send(ctx, pid, message) }

func Respond(ctx context.Context, request Request, message any, responseErr ...error) error {
	err := error(nil)
	if len(responseErr) > 0 { err = responseErr[0] }
	if runtime, runtimeErr := currentRuntime(); runtimeErr == nil { return runtime.Respond(ctx, request, message, err) }
	if app == nil { return errors.New("actor app is not initialized") }
	return app.respond(ctx, request, message, err)
}

func Call(ctx context.Context, pid PID, message any, timeout time.Duration) (any, error) {
	if runtime, err := currentRuntime(); err == nil { return runtime.Call(ctx, pid, message, timeout) }
	if app == nil { return nil, errors.New("actor app is not initialized") }
	return app.call(ctx, pid, message, timeout)
}

func CallSync(ctx context.Context, pid PID, message any, sender PID) error {
	if app == nil { return errors.New("actor app is not initialized") }
	return app.callSync(ctx, pid, message, sender)
}

func GetNodeName() string { if app == nil { return "" }; return app.GetNodeName() }
func StopActor(pid PID) error {
	if runtime, err := currentRuntime(); err == nil { return runtime.Stop(pid) }
	if app == nil { return errors.New("actor app is not initialized") }
	return app.StopActor(pid)
}
func Stop(pid PID) error { return StopActor(pid) }
func Host() string { if app == nil { return "" }; return app.Host() }
func Address() string { if app == nil { return "" }; return app.Address() }

func ActivateActor(ctx context.Context, kind string, id string, spawn bool) (PID, error) {
	if runtime, err := currentRuntime(); err == nil { return runtime.ActivateActor(ctx, kind, id, spawn) }
	if app == nil { return PID{}, errors.New("actor app is not initialized") }
	return app.ActivateActor(ctx, kind, id, spawn)
}
func GetActorOwner(ctx context.Context, kind string, id string) (ActorOwner, error) {
	if app == nil { return ActorOwner{}, errors.New("actor app is not initialized") }
	return app.GetActorOwner(ctx, kind, id)
}
func GetActorCount(kind string) int { if app == nil { return 0 }; return app.GetActorCount(kind) }
func GetLocalActor(kind string, id string) PID { if app == nil { return PID{} }; return app.GetLocalActor(kind, id) }
func GetLocalActorAll(kind string) []PID { if app == nil { return nil }; return app.GetLocalActorAll(kind) }
func ActorError(reason string) *pb.ActorError { return &pb.ActorError{Reason: reason} }
