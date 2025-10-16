package gxyactor

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxymodule"
	"gserver/protocol/pb"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/asynkron/protoactor-go/remote"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
)

// actorSystem 基础Actor模块
type actorSystem struct {
	gxymodule.Module
	system   *actor.ActorSystem
	remote   *remote.Remote
	nodeName string
	host     string
	grainMgr *grainManager
}

var actorSys *actorSystem

// ActorSystem 获取基础Actor模块
func ActorSystem() *actorSystem {
	return actorSys
}

// NewActorSystem创建基础Actor模块
func NewActorSystem(nodeName string, host string) *actorSystem {
	// split host from nodeName(name@host)
	actorSys = &actorSystem{
		nodeName: nodeName,
		host:     host,
	}

	return actorSys
}

// OnInit Actor模块初始化 - 启动节点
func (a *actorSystem) OnInit(ctx context.Context) error {
	var err error
	if err = a.Module.OnInit(ctx); err != nil {
		return err
	}
	a.system = actor.NewActorSystem(actor.WithLoggerFactory(glogAdapterLogging))
	config := remote.Configure(a.host, 0)
	a.remote = remote.NewRemote(a.system, config)
	a.remote.Start()
	a.grainMgr = NewGrainManager(a)
	if err := a.grainMgr.Init(ctx); err != nil {
		return err
	}
	return nil
}

func (a *actorSystem) OnStart(ctx context.Context) error {
	if err := a.Module.OnStart(ctx); err != nil {
		return err
	}
	// 启动服务
	return nil
}

func (a *actorSystem) GetActorSystem() *actor.ActorSystem {
	return a.system
}

func (a *actorSystem) RegisterGrain(name string, prod func() actor.Actor) error {
	return a.grainMgr.RegisterGrain(name, prod)
}

// OnStop 停止Actor模块 - 停止节点
func (a *actorSystem) OnStop(ctx context.Context) error {
	if a.system != nil {
		// 停止节点
		a.system.Shutdown()
		glog.Infof(ctx, "node stopped: %s", a.nodeName)
	}
	a.grainMgr.Stop(ctx)
	return a.Module.OnStop(ctx)
}

// SpawnRegister创建新的Actor
func (a *actorSystem) SpawnNamed(name string, prod func() actor.Actor) (PID, error) {
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

func (a *actorSystem) Spawn(prod func() actor.Actor) (pid PID, err error) {
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
func (a *actorSystem) Send(pid PID, message any) error {
	if a.system == nil {
		return fmt.Errorf("node not initialized")
	}
	a.system.Root.Send(pid, message)
	return nil
}

func (a *actorSystem) Call(pid PID, message any) (any, error) {
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

func (a *actorSystem) GetNodeName() string {
	return string(a.nodeName)
}

func (a *actorSystem) StopActor(pid PID) error {
	if a.system == nil {
		return fmt.Errorf("node not initialized")
	}
	a.system.Root.Stop(pid)
	return nil
}

func (a *actorSystem) Host() string {
	return a.host
}

func (a *actorSystem) Address() string {
	return a.system.Address()
}

func (a *actorSystem) GetGrain(kind string, id string, spawn ...bool) (PID, error) {
	spawnFlag := true
	if len(spawn) > 0 {
		spawnFlag = spawn[0]
	}
	return a.grainMgr.getGrain(kind, id, spawnFlag)
}

func newSupervisor() actor.SupervisorStrategy {
	return actor.NewOneForOneStrategy(10, 3*time.Second, decider)
}

func decider(reason any) actor.Directive {
	glog.Errorf(context.Background(), "actor panic: %v", reason)
	return actor.EscalateDirective
}

func PidEqual(a, b PID) bool {
	return a.Id == b.Id && a.Address == b.Address
}
