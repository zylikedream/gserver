package gxyactor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gserver/core/gxylocator"
	"gserver/core/gxymodule"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/asynkron/protoactor-go/remote"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
)

const defaultHostPort = 10410
const sytemName = "gxyactor"

// actorSystem 基础Actor模块
type actorSystem struct {
	gxymodule.Module
	system       *actor.ActorSystem
	remote       *remote.Remote
	nodeName     string
	host         string
	actorLocator *gxylocator.Locator
}

var actorSys *actorSystem

// ActorSystem 获取基础Actor模块
func ActorSystem() *actorSystem {
	return actorSys
}

// NewActorSystem创建基础Actor模块
func NewActorSystem(nodeName string) *actorSystem {
	// split host from nodeName(name@host)
	host := nodeName
	if idx := strings.Index(nodeName, "@"); idx > 0 {
		host = nodeName[idx+1:]
	}
	if host == "" {
		panic("invalid nodename, should like 'node@ip'")
	}
	actorSys = &actorSystem{
		nodeName:     nodeName,
		host:         host,
		actorLocator: gxylocator.NewLocator(sytemName, 30*time.Second),
	}
	return actorSys
}

// OnInit Actor模块初始化 - 启动节点
func (a *actorSystem) OnInit(ctx context.Context) error {
	var err error
	if err = a.Module.OnInit(ctx); err != nil {
		return err
	}
	a.system = actor.NewActorSystem()
	config := remote.Configure(a.host, defaultHostPort)
	a.remote = remote.NewRemote(a.system, config)
	a.remote.Start()
	return nil
}

func (a *actorSystem) GetActorSystem() *actor.ActorSystem {
	return a.system
}

func (a *actorSystem) RegisterKind(name string, prod func() actor.Actor) {
	a.remote.Register(name, actor.PropsFromProducer(prod))
}

func (a *actorSystem) RegisterActor(name string, prod func() actor.Actor) {
	a.remote.Register(name, actor.PropsFromProducer(prod))
}

// OnStop 停止Actor模块 - 停止节点
func (a *actorSystem) OnStop(ctx context.Context) error {
	if a.system != nil {
		// 停止节点
		a.system.Shutdown()
		glog.Infof(ctx, "node stopped: %s", a.nodeName)
	}
	return a.Module.OnStop(ctx)
}

// SpawnRegister创建新的Actor
func (a *actorSystem) SpawnNamed(name string, prod func() actor.Actor) (PID, error) {
	if a.system == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	props := actor.PropsFromProducer(prod)
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
	props := actor.PropsFromProducer(prod)
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
