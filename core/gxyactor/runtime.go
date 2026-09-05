package gxyactor

import (
	"errors"
	"context"
	"reflect"
	"sync"
	"time"
)

// PID is the runtime-neutral identity of an actor process. Runtime, Node, ID,
// and Creation all participate in identity; callers must not construct a PID
// by parsing a runtime-specific process identifier.
type PID struct {
	Runtime  string
	Node     string
	ID       string
	Creation string
}

func (p PID) IsZero() bool {
	return p.Runtime == "" && p.Node == "" && p.ID == "" && p.Creation == ""
}

func normalizePID(value any) PID {
	switch pid := value.(type) {
	case PID:
		return pid
	case *PID:
		if pid != nil { return *pid }
		return PID{}
	}
	if value == nil { return PID{} }
	v := reflect.Indirect(reflect.ValueOf(value))
	if v.IsValid() && v.Kind() == reflect.Struct {
		address, id := v.FieldByName("Address"), v.FieldByName("Id")
		if address.IsValid() && id.IsValid() && address.Kind() == reflect.String && id.Kind() == reflect.String {
			return PID{Runtime: "protoactor-v1", Node: address.String(), ID: id.String()}
		}
	}
	return PID{}
}

// Request is the opaque request handle available to an actor callback. It is
// deliberately smaller than a runtime context so adapters can implement it
// without exposing their process or envelope types.
type Request interface {
	Sender() PID
}

// ActorContext is the runtime-neutral callback context supplied by an adapter.
// Message delivery and lifecycle control stay behind this interface.
type ActorContext interface {
	Request
	Message() any
	MessageHeader() map[string]string
	Self() PID
	Stop(PID)
	Watch(PID)
	Unwatch(PID)
	Children() []PID
}

// Runtime owns process operations while the package exposes only normalized
// identities and opaque business values. Implementations live in runtime-
// private adapters (for example, the future Ergo adapter).
type Runtime interface {
	RegisterActorKind(string, ActorProducer) error
	DeregisterActorKind(string)
	ActivateActor(context.Context, string, string, bool) (PID, error)
	GetLocalActor(string, string) PID
	GetLocalActorAll(string) []PID
	Send(context.Context, PID, any) error
	LocalSend(context.Context, PID, any) error
	Call(context.Context, PID, any, time.Duration) (any, error)
	Respond(context.Context, Request, any, error) error
	Stop(PID) error
}

var (
	runtimeMu sync.RWMutex
	runtimeImpl Runtime
)

// SetRuntime installs the process runtime used by the package helpers. It is
// intended for runtime bootstrap and focused tests; production installs one
// adapter during actor application initialization.
func SetRuntime(runtime Runtime) {
	runtimeMu.Lock()
	runtimeImpl = runtime
	runtimeMu.Unlock()
}

func currentRuntime() (Runtime, error) {
	runtimeMu.RLock()
	runtime := runtimeImpl
	runtimeMu.RUnlock()
	if runtime == nil {
		return nil, errors.New("actor runtime is not initialized")
	}
	return runtime, nil
}

func PidEqual(a, b any) bool {
	pa, pb := normalizePID(a), normalizePID(b)
	if pa.IsZero() || pb.IsZero() {
		return false
	}
	return pa == pb
}

// LifecycleMessage is a runtime-neutral lifecycle marker. The adapter turns
// its private process callbacks into these values before invoking ActorBase.
type LifecycleMessage uint8

const (
	ActorStarted LifecycleMessage = iota + 1
	ActorStopping
	ActorStopped
	ActorAutoRespond
)

type ActorStartedMessage struct {
	Self     PID
	InitArgs []any
}

type ActorStoppedMessage struct {
	Err error
}

type ActorTerminatedMessage struct {
	Who PID
}
