package ergo

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	ergo "ergo.services/ergo"
	"ergo.services/ergo/gen"
	"google.golang.org/protobuf/proto"
	"gserver/core/gxyactor"
)

var (
	ErrUnknownPID            = errors.New("unknown actor PID")
	ErrTimeout               = errors.New("actor call timeout")
	ErrRemoteNodeUnavailable = errors.New("remote node unavailable")
	ErrActorInitFailed       = errors.New("actor initialization failed")
	ErrActorStopped          = errors.New("actor stopped")
	ErrActivationUnavailable = errors.New("actor activation is owned by GServer directory")
)

type Error struct {
	Kind  error
	Cause error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Kind.Error()
	}
	return e.Kind.Error() + ": " + e.Cause.Error()
}
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
func (e *Error) Is(target error) bool {
	return e != nil && (errors.Is(e.Kind, target) || errors.Is(e.Cause, target))
}
func wrap(kind, cause error) error {
	if cause == nil {
		return kind
	}
	return &Error{Kind: kind, Cause: cause}
}

type Options struct {
	NodeName         string
	NodeInstanceName string
	Host             string
	Port             int
	Cookie           string
	ShutdownTimeout  time.Duration
	Network          gen.NetworkOptions
	Activation       func(context.Context, string, string, bool) (gxyactor.PID, error)
}

type Adapter struct {
	node          gen.Node
	nodeName      string
	transportName string
	creation      int64
	registry      *MessageRegistry
	activation    func(context.Context, string, string, bool) (gxyactor.PID, error)

	mu         sync.RWMutex
	kinds      map[string]gxyactor.ActorProducer
	pids       map[string]gen.PID
	normalized map[string]gxyactor.PID
}

var _ gxyactor.Runtime = (*Adapter)(nil)

func New(node gen.Node, nodeInstanceName ...string) *Adapter {
	name := ""
	if len(nodeInstanceName) > 0 {
		name = nodeInstanceName[0]
	}
	a := &Adapter{node: node, nodeName: name, registry: NewMessageRegistry(), kinds: make(map[string]gxyactor.ActorProducer), pids: make(map[string]gen.PID), normalized: make(map[string]gxyactor.PID)}
	if node != nil {
		a.transportName = string(node.Name())
		if name == "" {
			name = a.transportName
			a.nodeName = name
		}
		a.creation = node.Creation()
		for _, control := range []any{
			GServerEnvelope{},
			gxyactor.ActorInitMsg{},
			gxyactor.ActorStartedMessage{},
			gxyactor.ActorStoppedMessage{},
			gxyactor.ActorTerminatedMessage{},
			gxyactor.LifecycleMessage(0),
		} {
			_ = node.Network().RegisterType(control)
		}
	}
	return a
}

func Start(options Options) (*Adapter, error) {
	name := options.NodeName
	if name == "" {
		return nil, errors.New("ergo node name is required")
	}
	network := options.Network
	if options.Cookie != "" {
		network.Cookie = options.Cookie
	}
	if options.Host != "" || options.Port != 0 {
		network.Acceptors = []gen.AcceptorOptions{{Host: options.Host, Port: uint16(options.Port)}}
	}
	node, err := ergo.StartNode(gen.Atom(name), gen.NodeOptions{ShutdownTimeout: options.ShutdownTimeout, Network: network})
	if err != nil {
		return nil, err
	}
	instance := options.NodeInstanceName
	if instance == "" {
		instance = name
	}
	a := New(node, instance)
	a.activation = options.Activation
	gxyactor.SetRuntime(a)
	return a, nil
}

func (a *Adapter) Node() gen.Node {
	if a == nil {
		return nil
	}
	return a.node
}
func (a *Adapter) Registry() *MessageRegistry {
	if a == nil {
		return nil
	}
	return a.registry
}
func (a *Adapter) StopNode(timeout time.Duration) {
	if a != nil && a.node != nil {
		a.node.StopWithTimeout(timeout)
	}
}

func (a *Adapter) RegisterMessageType(name string, constructor func() proto.Message) error {
	return a.registry.Register(name, constructor)
}

// RegisterActorKind stores only the factory. Ownership and activation remain in GServer.
func (a *Adapter) RegisterActorKind(kind string, producer gxyactor.ActorProducer) error {
	if kind == "" || producer == nil {
		return errors.New("actor kind and producer are required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.kinds[kind]; exists {
		return fmt.Errorf("actor kind %q already registered", kind)
	}
	a.kinds[kind] = producer
	return nil
}
func (a *Adapter) DeregisterActorKind(kind string) { a.mu.Lock(); delete(a.kinds, kind); a.mu.Unlock() }

func (a *Adapter) ActivateActor(ctx context.Context, kind, id string, allowSpawn bool) (gxyactor.PID, error) {
	if a.activation == nil {
		return gxyactor.PID{}, ErrActivationUnavailable
	}
	return a.activation(ctx, kind, id, allowSpawn)
}
func (a *Adapter) GetLocalActor(kind, id string) gxyactor.PID {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.normalized[kind+"\x00"+id]
}
func (a *Adapter) GetLocalActorAll(kind string) []gxyactor.PID {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]gxyactor.PID, 0)
	prefix := kind + "\x00"
	for key, pid := range a.normalized {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			result = append(result, pid)
		}
	}
	return result
}

func (a *Adapter) Spawn(kind, id string, initArgs ...any) (gxyactor.PID, error) {
	if kind == "" || id == "" {
		return gxyactor.PID{}, errors.New("actor kind and id are required")
	}
	a.mu.RLock()
	producer := a.kinds[kind]
	a.mu.RUnlock()
	if producer == nil {
		return gxyactor.PID{}, fmt.Errorf("%w: kind %q", ErrUnknownPID, kind)
	}
	if a.node == nil {
		return gxyactor.PID{}, errors.New("ergo node is not initialized")
	}
	logicalID := namespacedID(kind, id)
	pid, err := a.node.Spawn(func() gen.ProcessBehavior { return newErgoActor(a, kind, logicalID, producer) }, gen.ProcessOptions{}, initArgs...)
	if err != nil {
		return gxyactor.PID{}, wrap(ErrActorInitFailed, err)
	}
	normalized := a.fromErgoPID(pid, logicalID)
	a.mu.Lock()
	a.pids[kind+"\x00"+id] = pid
	a.normalized[kind+"\x00"+id] = normalized
	a.mu.Unlock()
	return normalized, nil
}

func (a *Adapter) rawPID(pid gxyactor.PID) (gen.PID, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, normalized := range a.normalized {
		if normalized == pid {
			for key, raw := range a.pids {
				if a.normalized[key] == pid {
					return raw, true
				}
			}
			break
		}
	}
	return gen.PID{}, false
}
func (a *Adapter) Forget(pid gxyactor.PID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, normalized := range a.normalized {
		if normalized == pid {
			delete(a.normalized, key)
			delete(a.pids, key)
		}
	}
}

func (a *Adapter) Send(ctx context.Context, pid gxyactor.PID, message any) error {
	raw, err := a.toErgoPID(pid)
	if err != nil {
		return err
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	wire, err := a.encodeFor(pid, message)
	if err != nil {
		return err
	}
	if a.node == nil {
		return errors.New("ergo node is not initialized")
	}
	if err := a.node.Send(raw, wire); err != nil {
		return mapError(err)
	}
	return nil
}
func (a *Adapter) LocalSend(ctx context.Context, pid gxyactor.PID, message any) error {
	return a.Send(ctx, pid, message)
}
func (a *Adapter) Call(ctx context.Context, pid gxyactor.PID, message any, timeout time.Duration) (any, error) {
	if a.node == nil {
		return nil, errors.New("ergo node is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	raw, err := a.toErgoPID(pid)
	if err != nil {
		return nil, err
	}
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	wire, err := a.encodeFor(pid, message)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout {
		timeout = time.Until(deadline)
	}
	type callResult struct {
		value any
		err   error
	}
	done := make(chan callResult, 1)
	seconds := int(timeout / time.Second)
	if timeout%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	go func() {
		value, callErr := a.node.CallWithTimeout(raw, wire, seconds)
		done <- callResult{value: value, err: callErr}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, wrap(ErrTimeout, ctx.Err())
	case <-timer.C:
		return nil, wrap(ErrTimeout, context.DeadlineExceeded)
	case result := <-done:
		if result.err != nil {
			return nil, mapError(result.err)
		}
		return a.decode(result.value)
	}
}
func (a *Adapter) Respond(_ context.Context, request gxyactor.Request, message any, responseErr error) error {
	r, ok := request.(*ergoRequest)
	if !ok || r == nil {
		return errors.New("request does not belong to Ergo runtime")
	}
	if responseErr != nil {
		return mapError(r.process.SendResponseError(r.rawSender, r.ref, responseErr))
	}
	wire, err := a.encodeFor(r.sender, message)
	if err != nil {
		return err
	}
	return mapError(r.process.SendResponse(r.rawSender, r.ref, wire))
}
func (a *Adapter) Stop(pid gxyactor.PID) error {
	raw, err := a.toErgoPID(pid)
	if err != nil {
		return nil
	}
	if a.node == nil {
		return nil
	}
	if err := a.node.SendExit(raw, gen.TerminateReasonNormal); err != nil && !errors.Is(err, gen.ErrProcessUnknown) {
		return mapError(err)
	}
	return nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return wrap(ErrTimeout, ctx.Err())
	default:
		return nil
	}
}
func mapError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, gen.ErrTimeout):
		return wrap(ErrTimeout, err)
	case errors.Is(err, gen.ErrProcessUnknown), errors.Is(err, gen.ErrProcessIncarnation):
		return wrap(ErrUnknownPID, err)
	case errors.Is(err, gen.ErrProcessTerminated):
		return wrap(ErrActorStopped, err)
	case errors.Is(err, gen.ErrNoConnection), errors.Is(err, gen.ErrNetworkStopped), errors.Is(err, gen.ErrNodeTerminated):
		return wrap(ErrRemoteNodeUnavailable, err)
	default:
		return err
	}
}
func (a *Adapter) encode(message any) (any, error) {
	if protoMessage, ok := message.(proto.Message); ok {
		envelope, err := a.registry.Encode(protoMessage)
		if err != nil {
			return nil, err
		}
		return envelope, nil
	}
	return message, nil
}
func (a *Adapter) decode(message any) (any, error) {
	if envelope, ok := message.(GServerEnvelope); ok {
		return a.registry.Decode(envelope)
	}
	if envelope, ok := message.(*GServerEnvelope); ok && envelope != nil {
		return a.registry.Decode(*envelope)
	}
	return message, nil
}
func (a *Adapter) encodeFor(pid gxyactor.PID, message any) (any, error) {
	if pid.Node == a.nodeName {
		return message, nil
	}
	return a.encode(message)
}
