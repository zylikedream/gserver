package ergo

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
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
	NodeName          string
	NodeInstanceName  string
	Host              string
	Port              int
	Cookie            string
	ShutdownTimeout   time.Duration
	Network           gen.NetworkOptions
	Activation        func(context.Context, string, string, bool) (gxyactor.PID, error)
	ResolvePID        func(gxyactor.PID) (gen.PID, error)
	RegisterActorKind func(string, gxyactor.ActorProducer) error
}

type Adapter struct {
	node          gen.Node
	nodeName      string
	transportName string
	creation      int64
	registry      *MessageRegistry
	activation    func(context.Context, string, string, bool) (gxyactor.PID, error)
	resolvePID    func(gxyactor.PID) (gen.PID, error)
	registerKind  func(string, gxyactor.ActorProducer) error
	host          string
	port          int

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
			gxyactor.ActorTerminatedMessage{},
			gxyactor.ActorPIDResponse{},
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
	a := New(node, instance)
	a.activation = options.Activation
	a.resolvePID = options.ResolvePID
	a.registerKind = options.RegisterActorKind
	a.host, a.port = options.Host, options.Port
	gxyactor.SetRuntime(a)
	return a, nil
}

func (a *Adapter) Node() gen.Node {
	if a == nil {
		return nil
	}
	return a.node
}

// NodeInstanceName is the canonical GServer identity used by ownership and
// service discovery. It is intentionally exposed as a string-only capability
// so business code never depends on Ergo node types.
func (a *Adapter) NodeInstanceName() string {
	if a == nil {
		return ""
	}
	return a.nodeName
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
	if a.registerKind != nil {
		return a.registerKind(kind, producer)
	}
	return a.RegisterActorKindDirect(kind, producer)
}

// RegisterActorKindDirect stores a factory without invoking the bootstrap hook.
func (a *Adapter) RegisterActorKindDirect(kind string, producer gxyactor.ActorProducer) error {
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
	pid, err := a.node.SpawnRegister(gen.Atom(logicalID), func() gen.ProcessBehavior {
		return newErgoActor(a, kind, id, logicalID, producer)
	}, gen.ProcessOptions{}, initArgs...)
	if err != nil {
		if pid.ID != 0 {
			a.forgetRaw(pid)
		}
		return gxyactor.PID{}, wrap(ErrActorInitFailed, err)
	}
	normalized := a.fromErgoPID(pid, logicalID)
	a.mu.Lock()
	a.pids[kind+"\x00"+id] = pid
	a.normalized[kind+"\x00"+id] = normalized
	a.mu.Unlock()
	return normalized, nil
}

// SpawnAnonymous creates an unregistered business process for one-off actors such as sessions.
func (a *Adapter) SpawnAnonymous(producer gxyactor.ActorProducer, initArgs ...any) (gxyactor.PID, error) {
	if producer == nil || a.node == nil {
		return gxyactor.PID{}, errors.New("actor runtime is not initialized")
	}
	pid, err := a.node.Spawn(func() gen.ProcessBehavior { return newErgoActor(a, "anonymous", "", "", producer) }, gen.ProcessOptions{}, initArgs...)
	if err != nil {
		return gxyactor.PID{}, wrap(ErrActorInitFailed, err)
	}
	normalized := a.fromErgoPID(pid, fmt.Sprintf("anonymous/%d", pid.ID))
	a.mu.Lock()
	a.pids["anonymous\x00"+normalized.ID] = pid
	a.normalized["anonymous\x00"+normalized.ID] = normalized
	a.mu.Unlock()
	return normalized, nil
}

// SpawnNamed creates a registered Ergo process used by GServer control actors.
func (a *Adapter) SpawnNamed(kind, name string, producer gxyactor.ActorProducer, initArgs ...any) (gxyactor.PID, error) {
	if producer == nil || a.node == nil {
		return gxyactor.PID{}, errors.New("actor runtime is not initialized")
	}
	pid, err := a.node.SpawnRegister(gen.Atom(name), func() gen.ProcessBehavior { return newErgoActor(a, kind, name, namespacedID(kind, name), producer) }, gen.ProcessOptions{}, initArgs...)
	if err != nil {
		return gxyactor.PID{}, wrap(ErrActorInitFailed, err)
	}
	normalized := a.fromErgoPID(pid, namespacedID(kind, name))
	a.mu.Lock()
	a.pids[kind+"\x00"+name] = pid
	a.normalized[kind+"\x00"+name] = normalized
	a.mu.Unlock()
	return normalized, nil
}

// CallNamed addresses a registered process on a node by canonical node identity.
func (a *Adapter) CallNamed(ctx context.Context, node, name string, message any, timeout time.Duration) (any, error) {
	if a.node == nil {
		return nil, errors.New("ergo node is not initialized")
	}
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	wire, err := a.encodeFor(gxyactor.PID{Runtime: RuntimeID, Node: node, ID: name, Creation: strconv.FormatInt(a.creation, 10)}, message)
	if err != nil {
		return nil, err
	}
	seconds := int(timeout / time.Second)
	if timeout%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	value, err := a.node.CallProcessID(gen.ProcessID{Name: gen.Atom(name), Node: gen.Atom(node)}, wire, seconds)
	if err != nil {
		return nil, mapError(err)
	}
	return a.decode(value)
}

func (a *Adapter) CallSync(ctx context.Context, pid gxyactor.PID, message any, sender gxyactor.PID) error {
	return a.Send(ctx, pid, message)
}
func (a *Adapter) NodeName() string { return a.nodeName }
func (a *Adapter) Host() string     { return a.host }
func (a *Adapter) Address() string {
	if a.host == "" || a.port == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", a.host, a.port)
}

func (a *Adapter) remember(kind, id string, raw gen.PID) {
	if a == nil || raw.ID == 0 {
		return
	}
	normalized := a.fromErgoPID(raw, id)
	key := kind + "\x00" + id
	a.mu.Lock()
	a.pids[key] = raw
	a.normalized[key] = normalized
	a.mu.Unlock()
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

func (a *Adapter) forgetRaw(raw gen.PID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, pid := range a.pids {
		if pid == raw {
			delete(a.pids, key)
			delete(a.normalized, key)
		}
	}
}

func (a *Adapter) target(pid gxyactor.PID) (gen.PID, bool, error) {
	raw, err := a.toErgoPID(pid)
	if err == nil {
		return raw, false, nil
	}
	// Named business and control processes are addressable on remote nodes
	// without a local PID table entry. Anonymous actors must be local-only.
	if pid.Runtime == RuntimeID && pid.Node != "" && pid.Node != a.nodeName &&
		pid.ID != "" && strings.Contains(pid.ID, "/") {
		return gen.PID{}, true, nil
	}
	return gen.PID{}, false, err
}

func (a *Adapter) Send(ctx context.Context, pid gxyactor.PID, message any) error {
	raw, processID, err := a.target(pid)
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
	if process, ok := processFromContext(ctx); ok {
		if processID {
			return mapError(process.SendProcessID(gen.ProcessID{Name: gen.Atom(pid.ID), Node: gen.Atom(pid.Node)}, wire))
		}
		return mapError(process.Send(raw, wire))
	}
	if a.node == nil {
		return errors.New("ergo node is not initialized")
	}
	var sendErr error
	if processID {
		sendErr = a.node.Send(gen.ProcessID{Name: gen.Atom(pid.ID), Node: gen.Atom(pid.Node)}, wire)
	} else {
		sendErr = a.node.Send(raw, wire)
	}
	if sendErr != nil {
		return mapError(sendErr)
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
	raw, processID, err := a.target(pid)
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
	process, fromActor := processFromContext(ctx)
	go func() {
		var value any
		var callErr error
		if fromActor {
			if processID {
				value, callErr = process.CallProcessID(gen.ProcessID{Name: gen.Atom(pid.ID), Node: gen.Atom(pid.Node)}, wire, seconds)
			} else {
				value, callErr = process.CallWithTimeout(raw, wire, seconds)
			}
		} else {
			if processID {
				value, callErr = a.node.CallProcessID(gen.ProcessID{Name: gen.Atom(pid.ID), Node: gen.Atom(pid.Node)}, wire, seconds)
			} else {
				value, callErr = a.node.CallWithTimeout(raw, wire, seconds)
			}
		}
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
	var r *ergoRequest
	switch value := request.(type) {
	case *ergoRequest:
		r = value
	case *actorContext:
		if value != nil {
			r = value.request
		}
	}
	if r == nil {
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

func processFromContext(ctx context.Context) (gen.Process, bool) {
	if ctx == nil {
		return nil, false
	}
	process, ok := ctx.Value(processContextKey{}).(gen.Process)
	return process, ok && process != nil
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
	case errors.Is(err, gen.ErrNoConnection), errors.Is(err, gen.ErrNoRoute), errors.Is(err, gen.ErrNetworkStopped), errors.Is(err, gen.ErrNodeTerminated):
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
