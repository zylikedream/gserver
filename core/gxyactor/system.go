package gxyactor

import (
	"context"
	"sync"
	"time"
	"gserver/core/gxyapp"
	"gserver/core/gxylog"
	"gserver/protocol/pb"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"github.com/asynkron/protoactor-go/actor"
	"github.com/asynkron/protoactor-go/remote"
	"github.com/cockroachdb/errors"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

type actorApp struct { gxyapp.App; system *actor.ActorSystem; remote *remote.Remote; nodeName, nodeInstanceName, host string; activatorMgr *activatorManager }
var legacyPIDByNormalized sync.Map
const CLUSTER_NAME = "gcluster"
var app *actorApp
func ActorApp() *actorApp { return app }
func (a *actorApp) NodeName() string { return a.nodeName }
func NewActorApp(nodeName, nodeInstanceName, host string) *actorApp { app = &actorApp{nodeName: nodeName, nodeInstanceName: nodeInstanceName, host: host}; SetRuntime(app); return app }
func (a *actorApp) newSystem() *actor.ActorSystem { return actor.NewActorSystem(actor.WithLoggerFactory(glogAdapterLogging)) }
func (a *actorApp) OnModInit(ctx context.Context) error { port := g.Cfg().MustGet(ctx, "port.actor").Int(); a.system = a.newSystem(); a.remote = remote.NewRemote(a.system, remote.Configure(a.host, port)); a.remote.Start(); a.activatorMgr = NewActivatorManager(a.nodeName, a.nodeInstanceName); return a.AddModule(ctx, a.activatorMgr) }
func (a *actorApp) OnModStart(ctx context.Context) error { gxylog.Info(ctx, "actor started ", gxylog.Str("nodeName", a.nodeName), gxylog.Str("address", a.Address())); return nil }
func (a *actorApp) RegisterActorKind(name string, prod ActorProducer) error { if a.activatorMgr == nil { return errors.New("actor app is not initialized") }; return a.activatorMgr.RegisterActorKind(name, prod) }
func (a *actorApp) DeregisterActorKind(name string) { if a.activatorMgr != nil { a.activatorMgr.DeregisterActorKind(name) } }
func (a *actorApp) spawnNamedLegacy(props *actor.Props, name string, initArgs ...any) (PID, error) { p := props; if len(initArgs)>0 { p=p.Configure(actor.WithContextDecorator(legacyContextDecorator(initArgs...))) }; if a.system==nil { return PID{}, errors.New("node not initialized") }; pid, err := a.system.Root.SpawnNamed(p,name); if err != nil{return PID{},err}; return pidFromProto(pid,a),nil }
func (a *actorApp) spawnLegacy(props *actor.Props, initArgs ...any) (PID,error) { p:=props; if a.system==nil{return PID{},errors.New("node not initialized")}; if len(initArgs)>0{p=p.Configure(actor.WithContextDecorator(legacyContextDecorator(initArgs...)))}; return pidFromProto(a.system.Root.Spawn(p),a),nil }
func (a *actorApp) spawnFunc(prod ActorProducer, initArgs ...any) (PID,error) { return a.spawnLegacy(actor.PropsFromProducer(legacyActorProducer(prod, a)), initArgs...) }
func (a *actorApp) Send(ctx context.Context,pid PID,message any) error{return a.send(ctx,pid,message)}
func (a *actorApp) LocalSend(ctx context.Context,pid PID,message any) error{return a.localSend(ctx,pid,message)}
func (a *actorApp) localSend(ctx context.Context,pid PID,message any) error{return a.send(ctx,pid,message)}
func (a *actorApp) send(ctx context.Context,pid PID,message any) error {if a.system==nil{return errors.New("node not initialized")}; if env:=injectTrace(ctx,message);env!=nil{a.system.Root.Send(protoFromPID(pid),env);return nil};a.system.Root.Send(protoFromPID(pid),message);return nil}
func (a *actorApp) Respond(ctx context.Context,request Request,message any,responseErr error) error{return a.respond(ctx,request,message,responseErr)}
func (a *actorApp) respond(ctx context.Context,request Request,message any,responseErr error) error {if request==nil||request.Sender().IsZero(){gxylog.Warn(ctx,"sender is nil, can not respond");return responseErr};if responseErr!=nil{message=&pb.ActorError{Reason:responseErr.Error()}};return a.send(ctx,request.Sender(),message)}
func (a *actorApp) Call(ctx context.Context,pid PID,message any,timeout time.Duration)(any,error){return a.call(ctx,pid,message,timeout)}
func (a *actorApp) call(ctx context.Context,pid PID,message any,timeout time.Duration)(any,error){if a.system==nil{return nil,errors.New("node not initialized")};future:=actor.NewFuture(a.system,timeout);if err:=a.send(ctx,pid,&actor.MessageEnvelope{Message:message,Sender:future.PID()});err!=nil{return nil,err};result,err:=future.Result();if err!=nil{return nil,err};if aerr,ok:=result.(*pb.ActorError);ok{return nil,gerror.New(aerr.Reason)};return result,nil}
func (a *actorApp) callSync(ctx context.Context,pid PID,message any,sender PID) error{return a.send(ctx,pid,&actor.MessageEnvelope{Message:message,Sender:protoFromPID(sender)})}
func (a *actorApp) GetNodeName()string{return a.nodeName}
func (a *actorApp) Stop(pid PID)error{if a.system==nil{return errors.New("node not initialized")};a.system.Root.Stop(protoFromPID(pid));return nil}
func (a *actorApp) StopActor(pid PID)error{return a.Stop(pid)}
func (a *actorApp) Host()string{return a.host}
func (a *actorApp) NodeInstanceName()string{return a.nodeInstanceName}
func (a *actorApp) GetActorOwner(ctx context.Context,kind,id string)(ActorOwner,error){if a.activatorMgr==nil||a.activatorMgr.locator==nil{return ActorOwner{},errors.New("actor locator is not initialized")};return a.activatorMgr.locator.locate(ctx,kind,id)}
func (a *actorApp) Address()string{if a.system==nil{return ""};return a.system.Address()}
func (a *actorApp) ActivateActor(ctx context.Context,kind,id string,spawn bool)(PID,error){return a.activatorMgr.getActor(ctx,kind,id,spawn)}
func (a *actorApp) GetLocalActor(kind,id string)PID{return a.activatorMgr.GetLocalActor(kind,id)}
func (a *actorApp) GetLocalActorAll(kind string)[]PID{return a.activatorMgr.GetLocalActorAll(kind)}
// Protoactor does not expose an incarnation value through this compatibility
// path, so Creation remains empty until the Ergo adapter supplies one.
func pidFromProto(pid *actor.PID,a *actorApp)PID{if pid==nil{return PID{}};node:=pid.Address;if a!=nil&&a.nodeInstanceName!=""&&a.system!=nil&&pid.Address==a.system.Address(){node=a.nodeInstanceName};normalized:=PID{Runtime:"protoactor-v1",Node:node,ID:pid.Id};legacyPIDByNormalized.Store(normalized,pid);return normalized}
func protoFromPID(pid PID)*actor.PID{if pid.IsZero(){return nil};if legacy,ok:=legacyPIDByNormalized.Load(pid);ok{return legacy.(*actor.PID)};return actor.NewPID(pid.Node,pid.ID)}
type messageEnvelopeCarrier struct{envelope *actor.MessageEnvelope}
func(c messageEnvelopeCarrier)Get(key string)string{if c.envelope==nil||c.envelope.Header==nil{return ""};return c.envelope.Header.Get(key)}
func(c messageEnvelopeCarrier)Set(key,val string){if c.envelope!=nil{c.envelope.SetHeader(key,val)}}
func(c messageEnvelopeCarrier)Keys()[]string{if c.envelope==nil||c.envelope.Header==nil{return nil};return c.envelope.Header.Keys()}
func injectTrace(ctx context.Context,msg any)*actor.MessageEnvelope{env:=&actor.MessageEnvelope{Message:msg};if existing,ok:=msg.(*actor.MessageEnvelope);ok{env=existing};span:=trace.SpanFromContext(ctx);if !span.SpanContext().IsValid(){return nil};otel.GetTextMapPropagator().Inject(ctx,messageEnvelopeCarrier{env});return env}
func newSupervisor()actor.SupervisorStrategy{return actor.NewOneForOneStrategy(10,3*time.Second,decider)}
func decider(reason any)actor.Directive{gxylog.Error(context.Background(),"actor error",gxylog.Any("reason",reason));return actor.StopDirective}
func legacyContextDecorator(args ...any)actor.ContextDecorator{return func(next actor.ContextDecoratorFunc)actor.ContextDecoratorFunc{return func(ctx actor.Context)actor.Context{return &legacyActorContext{Context:ctx,InitArgs:args}}}}
type legacyActorContext struct{actor.Context;InitArgs []any}

type legacyActorAdapter struct{actor IActor;app *actorApp}
func(a *legacyActorAdapter)Receive(ctx actor.Context){receiver,ok:=a.actor.(interface{Receive(ActorContext)});if ok{receiver.Receive(&legacyActorContextAdapter{Context:ctx,app:a.app})}}
func legacyActorProducer(prod func()IActor,app *actorApp)func()actor.Actor{return func()actor.Actor{return &legacyActorAdapter{actor:prod(),app:app}}}
type legacyActorContextAdapter struct{actor.Context;app *actorApp}
func(c *legacyActorContextAdapter)Message()any{msg:=c.Context.Message();switch value:=msg.(type){case *actor.Started:args:=[]any(nil);if decorated,ok:=c.Context.(*legacyActorContext);ok{args=decorated.InitArgs};return ActorStartedMessage{Self:pidFromProto(c.Context.Self(),c.app),InitArgs:args};case *actor.Stopping:return ActorStopping;case *actor.Stopped:return ActorStoppedMessage{};case actor.AutoRespond:return ActorAutoRespond;case *actor.Terminated:return ActorTerminatedMessage{Who:pidFromProto(value.Who,c.app)};case *actor.MessageEnvelope:return value.Message;default:return msg}}
func(c *legacyActorContextAdapter)Sender()PID{return pidFromProto(c.Context.Sender(),c.app)}
func(c *legacyActorContextAdapter)Self()PID{return pidFromProto(c.Context.Self(),c.app)}
func(c *legacyActorContextAdapter)MessageHeader()map[string]string{header:=c.Context.MessageHeader();if header==nil{return nil};return header.ToMap()}
func(c *legacyActorContextAdapter)Stop(pid PID){c.Context.Stop(protoFromPID(pid))}
func(c *legacyActorContextAdapter)Watch(pid PID){c.Context.Watch(protoFromPID(pid))}
func(c *legacyActorContextAdapter)Unwatch(pid PID){c.Context.Unwatch(protoFromPID(pid))}
func(c *legacyActorContextAdapter)Children()[]PID{children:=c.Context.Children();result:=make([]PID,0,len(children));for _,child:=range children{result=append(result,pidFromProto(child,c.app))};return result}
