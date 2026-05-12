package gxyactor

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxyapp"
	"gserver/core/gxylog"
	"gserver/protocol/pb"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/asynkron/protoactor-go/remote"
	"github.com/gogf/gf/v2/errors/gerror"
)

// actorApp 基础Actor模块
type actorApp struct {
	gxyapp.App
	system           *actor.ActorSystem
	remote           *remote.Remote
	nodeName         string
	nodeInstanceName string
	host             string
	activatorMgr     *activatorManager
}

const (
	CLUSTER_NAME = "gcluster"
)

var app *actorApp

// ActorSystem 获取基础Actor模块
func ActorApp() *actorApp {
	return app
}

func (a *actorApp) NodeName() string {
	return a.nodeName
}

// NewActorSystem创建基础Actor模块
func NewActorApp(nodeName string, nodeInstanceName string, host string) *actorApp {
	app = &actorApp{
		nodeName:         nodeName,
		nodeInstanceName: nodeInstanceName,
		host:             host,
	}

	return app
}

func (a *actorApp) newSystem() *actor.ActorSystem {
	return actor.NewActorSystem(actor.WithLoggerFactory(glogAdapterLogging))
}

// OnModInit Actor模块初始化 - 启动节点
func (a *actorApp) OnModInit(ctx context.Context) error {
	a.system = a.newSystem()
	config := remote.Configure(a.host, 0)
	a.remote = remote.NewRemote(a.system, config)
	a.remote.Start()
	a.activatorMgr = NewActivatorManager(a.nodeName, a.nodeInstanceName)
	a.AddModule(ctx, a.activatorMgr)
	return nil
}

func (a *actorApp) OnModStart(ctx context.Context) error {
	gxylog.Info(ctx, "actor started ", gxylog.Str("nodeName", a.nodeName), gxylog.Str("address", a.Address()))
	// 启动服务
	return nil
}

// OnModStop 停止Actor模块 - 停止节点
func (a *actorApp) OnModStop(ctx context.Context) error {
	a.system.Shutdown()
	gxylog.Info(ctx, "actor system stopped ", gxylog.Str("address", a.Address()))
	return nil
}

func (a *actorApp) RegisterActorKind(name string, prod ActorProducer) error {
	return a.activatorMgr.RegisterActorKind(name, prod)
}

func (a *actorApp) DeregisterActorKind(name string) {
	a.activatorMgr.DeregisterActorKind(name)
}

// SpawnRegister创建新的Actor
func (a *actorApp) spawnNamed(props *actor.Props, name string, initArgs ...any) (PID, error) {
	if len(initArgs) > 0 {
		props = props.Configure(actor.WithContextDecorator(ContextDecorator(initArgs...)))
	}
	return a.system.Root.SpawnNamed(props, name)
}

func (a *actorApp) spawn(props *actor.Props, initArgs ...any) (PID, error) {
	if a.system == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	if len(initArgs) > 0 {
		props = props.Configure(actor.WithContextDecorator(ContextDecorator(initArgs...)))
	}
	return a.system.Root.Spawn(props), nil
}

// Send 发送消息
func (a *actorApp) send(ctx context.Context, pid PID, message any) error {
	if a.system == nil {
		return fmt.Errorf("node not initialized")
	}
	if env := injectTrace(ctx, message); env != nil {
		a.system.Root.Send(pid, env)
		return nil
	}
	a.system.Root.Send(pid, message)
	return nil
}

// LocalSend 发送消息给本地actor, 不经过序列化, 所以可以发送any
func (a *actorApp) localSend(ctx context.Context, pid PID, message any) error {
	return a.send(ctx, pid, message)
}

func (a *actorApp) respond(ctx context.Context, actx actor.Context, message any) error {
	sender := actx.Sender()
	if sender == nil {
		gxylog.Warn(ctx, "sener is nil, can not respond")
		return nil
	}
	return a.send(ctx, sender, message)
}

func (a *actorApp) call(ctx context.Context, pid PID, message any, timeout time.Duration) (any, error) {
	// Extract trace headers and unwrap inner message
	future := actor.NewFuture(a.system, timeout)
	env := &actor.MessageEnvelope{
		Message: message,
		Sender:  future.PID(),
	}
	err := a.send(ctx, pid, env)
	if err != nil {
		return nil, err
	}
	result, err := future.Result()
	if err != nil {
		return nil, err
	}
	if aerr, ok := result.(*pb.ActorError); ok {
		return nil, gerror.New(aerr.Reason)
	}
	return result, nil
}

func (a *actorApp) callSync(ctx context.Context, pid PID, message any, sender PID) error {
	env := &actor.MessageEnvelope{
		Message: message,
		Sender:  sender,
	}
	return a.send(ctx, pid, env)
}

func (a *actorApp) GetNodeName() string {
	return string(a.nodeName)
}

func (a *actorApp) StopActor(pid PID) error {
	if a.system == nil {
		return fmt.Errorf("node not initialized")
	}
	a.system.Root.Stop(pid)
	return nil
}

func (a *actorApp) Host() string {
	return a.host
}

func (a *actorApp) NodeInstanceName() string {
	return a.nodeInstanceName
}

func (a *actorApp) Address() string {
	return a.system.Address()
}

func (a *actorApp) ActivateActor(ctx context.Context, kind string, id string, spawn bool) (PID, error) {
	return a.activatorMgr.getActor(ctx, kind, id, spawn)
}

func (a *actorApp) GetActorCount(kind string) int {
	return a.activatorMgr.GetActorCount(kind)
}

type messageEnvelopeCarrier struct {
	envelope *actor.MessageEnvelope
}

func (c messageEnvelopeCarrier) Get(key string) string {
	if c.envelope == nil || c.envelope.Header == nil {
		return ""
	}
	return c.envelope.Header.Get(key)
}

func (c messageEnvelopeCarrier) Set(key, val string) {
	if c.envelope != nil {
		c.envelope.SetHeader(key, val)
	}
}

func (c messageEnvelopeCarrier) Keys() []string {
	if c.envelope == nil || c.envelope.Header == nil {
		return nil
	}
	return c.envelope.Header.Keys()
}

// injectTrace injects OpenTelemetry trace context from ctx into a MessageEnvelope.
// Returns nil when ctx has no valid span (no trace to propagate).
func injectTrace(ctx context.Context, msg any) *actor.MessageEnvelope {
	env := &actor.MessageEnvelope{}
	switch msg := msg.(type) {
	case *actor.MessageEnvelope:
		env = msg
	default:
		env.Message = msg
	}
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return nil
	}
	carrier := messageEnvelopeCarrier{envelope: env}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return env
}

func newSupervisor() actor.SupervisorStrategy {
	return actor.NewOneForOneStrategy(10, 3*time.Second, decider)
}

func decider(reason any) actor.Directive {
	gxylog.Error(context.Background(), "actor error", gxylog.Any("reason", reason))
	return actor.StopDirective
}

func PidEqual(a, b PID) bool {
	if a == nil || b == nil {
		return false
	}
	return a.Id == b.Id && a.Address == b.Address
}
