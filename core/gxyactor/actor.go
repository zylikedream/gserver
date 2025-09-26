package gxyactor

import (
	"context"
	"fmt"

	"gserver/core/gxymodule"
	"gserver/core/gxyregistery"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/tochemey/goakt/v3/actor"
	"github.com/tochemey/goakt/v3/address"
	"github.com/tochemey/goakt/v3/remote"
	"google.golang.org/protobuf/proto"
)

const systemName = "gserver"
const defaultHostPort = 10410

// actorSystem 基础Actor模块
type actorSystem struct {
	gxymodule.Module
	system   actor.ActorSystem
	nodeName string
}

var actorSys *actorSystem

// ActorSystem 获取基础Actor模块
func ActorSystem() *actorSystem {
	return actorSys
}

// NewActorSystem创建基础Actor模块
func NewActorSystem(nodeName string) *actorSystem {
	actorSys = &actorSystem{
		nodeName: nodeName,
	}
	return actorSys
}

// OnInit Actor模块初始化 - 启动节点
func (a *actorSystem) OnInit(ctx context.Context) error {
	var err error
	if err = a.Module.OnInit(ctx); err != nil {
		return err
	}
	a.system, err = actor.NewActorSystem(systemName, actor.WithRemote(remote.NewConfig(
		a.nodeName,
		defaultHostPort,
	)))
	if err != nil {
		return err
	}
	return nil
}

func (a *actorSystem) GetActorSystem() actor.ActorSystem {
	return a.system
}

// OnStop 停止Actor模块 - 停止节点
func (a *actorSystem) OnStop(ctx context.Context) error {
	if a.system != nil {
		// 停止节点
		a.system.Stop(ctx)
		glog.Infof(ctx, "Ergo node stopped: %s", a.nodeName)
	}
	return a.Module.OnStop(ctx)
}

// SpawnRegister创建新的Actor
func (a *actorSystem) Spawn(name string, actor actor.Actor, options ...actor.SpawnOption) (PID, error) {
	if a.system == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	ctx := context.Background()
	pid, err := a.system.Spawn(ctx, name, actor, options...)
	if err != nil {
		return nil, gerror.Wrap(err, "failed to spawn actor")
	}

	return pid, nil
}

// Send 发送消息（异步）
func (a *actorSystem) Send(pid PID, message proto.Message) error {
	if a.system == nil {
		return fmt.Errorf("node not initialized")
	}

	return actor.Tell(context.Background(), pid, message)
}

// GetNodeName 获取节点名称
func (a *actorSystem) GetNodeName() string {
	return string(a.nodeName)
}

func (a *actorSystem) StopActor(pid PID) error {
	if a.system == nil {
		return fmt.Errorf("node not initialized")
	}
	err := a.system.Kill(context.Background(), pid.Name())
	return err
}

func (a *actorSystem) GetAddress(node gxyregistery.ServiceNode) *address.Address {
	return address.New(node.Name, systemName, node.Node, defaultHostPort)
}
