package gxyactor

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxyapp.go"

	"google.golang.org/protobuf/proto"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/asynkron/protoactor-go/remote"
	"github.com/gogf/gf/v2/os/glog"
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
func ActorApp() *actorApp {
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
	a.grainMgr = NewGrainManager()
	a.AddModule(ctx, a.grainMgr)
	return nil
}

func (a *actorApp) OnModStart(ctx context.Context) error {
	glog.Infof(ctx, "actor %s started at %s", a.nodeName, a.Address())
	// 启动服务
	return nil
}

// OnModStop 停止Actor模块 - 停止节点
func (a *actorApp) OnModStop(ctx context.Context) error {
	a.system.Shutdown()
	glog.Infof(ctx, "actor system stopped: %s", a.Address())
	return nil
}

func (a *actorApp) RegisterGrain(name string, prod GrainProducer) error {
	return a.grainMgr.RegisterGrain(name, prod)
}

func (a *actorApp) DeRegisterGrain(name string) {
	a.grainMgr.DeRegisterGrain(name)
}

// SpawnRegister创建新的Actor
func (a *actorApp) spawnNamed(name string, props *actor.Props) (PID, error) {
	return a.system.Root.SpawnNamed(props, name)
}

func (a *actorApp) spawn(props *actor.Props) (PID, error) {
	if a.system == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	return a.system.Root.Spawn(props), nil
}

// notice root调用request没有意义，,因为无法处理root进程的消息，就是接收方调用respond也无法处理(其实root进程的request和send方法时一样的)

// Send, call都是用于非actor向actor发送消息
// Send 发送消息（异步）
func (a *actorApp) send(pid PID, message proto.Message) error {
	if a.system == nil {
		return fmt.Errorf("node not initialized")
	}
	a.system.Root.Send(pid, message)
	return nil
}

func (a *actorApp) call(pid PID, message proto.Message, timeout time.Duration) (any, error) {
	if a.system == nil {
		return nil, fmt.Errorf("node not initialized")
	}

	return a.system.Root.RequestFuture(pid, message, timeout).Result()
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
