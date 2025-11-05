package gxyactor

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxyapp.go"
	"gserver/protocol/pb"
	"gserver/util"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/asynkron/protoactor-go/remote"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// actorApp 基础Actor模块
type actorApp struct {
	gxyapp.App
	system   *actor.ActorSystem
	remote   *remote.Remote
	nodeName string
	host     string
	grainMgr *grainManager
}

const (
	CLUSTER_NAME = "gcluster"
)

var app *actorApp

// ActorSystem 获取基础Actor模块
func ActorSystem() *actorApp {
	return app
}

func (a *actorApp) NodeName() string {
	return a.nodeName
}

// NewActorSystem创建基础Actor模块
func NewActorApp(nodeName string, host string) *actorApp {
	// split host from nodeName(name@host)
	app = &actorApp{
		nodeName: nodeName,
		host:     host,
	}

	return app
}

// OnModInit Actor模块初始化 - 启动节点
func (a *actorApp) OnModInit(ctx context.Context) error {
	a.system = actor.NewActorSystem(actor.WithLoggerFactory(glogAdapterLogging))
	config := remote.Configure(a.host, 0)
	a.remote = remote.NewRemote(a.system, config)
	a.remote.Start()
	a.grainMgr = NewGrainManager(a)
	a.AddModule(ctx, a.grainMgr)
	return nil
}

func (a *actorApp) OnModStart(ctx context.Context) error {
	glog.Infof(ctx, "actor %s started at %s", a.nodeName, a.Address())
	// 启动服务
	return nil
}

func (a *actorApp) RegisterGrain(name string, prod GrainProducer) error {
	return a.grainMgr.RegisterGrain(name, prod)
}

func (a *actorApp) DeRegisterGrain(name string) {
	a.grainMgr.DeRegisterGrain(name)
}

// OnModStop 停止Actor模块 - 停止节点
func (a *actorApp) OnModStop(ctx context.Context) error {
	a.system.Shutdown()
	glog.Infof(ctx, "actor system stopped: %s", a.Address())
	return nil
}

// SpawnRegister创建新的Actor
func (a *actorApp) SpawnNamed(name string, prod func() actor.Actor) (PID, error) {
	if a.system == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	actor.WithOnInit()
	props := actor.PropsFromProducer(prod, actor.WithSupervisor(newSupervisor()))
	pid, err := a.system.Root.SpawnNamed(props, name)
	if err != nil {
		return nil, gerror.Wrap(err, "failed to spawn actor")
	}

	return pid, nil
}

func (a *actorApp) Spawn(prod func() actor.Actor) (pid PID, err error) {
	if a.system == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	defer func() {
		if r := recover(); r != nil {
			glog.Errorf(context.Background(), "actor spawn panicked: %v", r)
			err = gerror.New("spawn error")
		}
	}()
	props := actor.PropsFromProducer(prod, actor.WithSupervisor(newSupervisor()))
	pid = a.system.Root.Spawn(props)
	return
}

// Send 发送消息（异步）
func (a *actorApp) Send(pid PID, message any) error {
	if a.system == nil {
		return fmt.Errorf("node not initialized")
	}
	a.system.Root.Send(pid, message)
	return nil
}

func (a *actorApp) Call(pid PID, message any) (any, error) {
	if a.system == nil {
		return nil, fmt.Errorf("node not initialized")
	}

	future := a.system.Root.RequestFuture(pid, message, 1115*time.Second)
	res, err := future.Result()
	if err != nil {
		return nil, gerror.Wrap(err, "call error")
	}
	if err, ok := res.(*pb.ActorError); ok {
		return nil, gerror.New(err.Reason)
	}
	return res, nil
}

func (a *actorApp) RpcCall(pid PID, message proto.Message) (proto.Message, error) {
	if a.system == nil {
		return nil, fmt.Errorf("node not initialized")
	}

	rpc := &pb.RpcMessage{
		Msg: &anypb.Any{},
	}

	if err := anypb.MarshalFrom(rpc.Msg, message, proto.MarshalOptions{}); err != nil {
		return nil, gerror.Newf("marshal rpc req error, err: %v", err)
	}
	res, err := a.Call(pid, rpc)
	if err != nil {
		return nil, gerror.Wrap(err, "rpc call error")
	}
	pres, _ := res.(proto.Message)
	return pres, nil
}

func (a *actorApp) Notify(pid PID, message any) error {
	if a.system == nil {
		return fmt.Errorf("node not initialized")
	}
	msgData, err := util.EncodeMsg(message)
	if err != nil {
		return gerror.Wrapf(err, "encode message error, msg: %v", message)
	}
	msg := &pb.PushMessage{
		MsgName: util.GetObjectName(message),
		MsgData: string(msgData),
	}
	a.system.Root.Send(pid, msg)
	return nil
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

func (a *actorApp) Address() string {
	return a.system.Address()
}

func (a *actorApp) GetGrain(kind string, id string, spawn ...bool) (PID, error) {
	spawnFlag := true
	if len(spawn) > 0 {
		spawnFlag = spawn[0]
	}
	return a.grainMgr.getGrain(kind, id, spawnFlag)
}

func (a *actorApp) GetGrainCount(kind string) int {
	return a.grainMgr.GetGrainCount(kind)
}

func newSupervisor() actor.SupervisorStrategy {
	return actor.NewOneForOneStrategy(10, 3*time.Second, decider)
}

func decider(reason any) actor.Directive {
	glog.Errorf(context.Background(), "actor error : %v", reason)
	return actor.StopDirective
}

func PidEqual(a, b PID) bool {
	if a == nil || b == nil {
		return false
	}
	return a.Id == b.Id && a.Address == b.Address
}
