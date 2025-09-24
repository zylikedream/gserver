package gxyactor

import (
	"context"
	"fmt"

	"gserver/core/gxymodule"

	"ergo.services/ergo"
	"ergo.services/ergo/gen"
	"ergo.services/registrar/etcd"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
)

// actorSystem 基础Actor模块
type actorSystem struct {
	gxymodule.Module
	node     gen.Node
	nodeName gen.Atom
	cookie   string
}

var actorSys *actorSystem

// ActorSystem 获取基础Actor模块
func ActorSystem() *actorSystem {
	return actorSys
}

// NewActorSystem创建基础Actor模块
func NewActorSystem(nodeName gen.Atom, cookie string) *actorSystem {
	actorSys = &actorSystem{
		Module:   gxymodule.Module{},
		nodeName: nodeName,
		cookie:   cookie,
	}
	return actorSys
}

// OnInit Actor模块初始化 - 启动节点
func (a *actorSystem) OnInit(ctx context.Context) error {
	if err := a.Module.OnInit(ctx); err != nil {
		return err
	}
	registrarOptions := etcd.Options{
		Endpoints: []string{"localhost:2379"},
		Cluster:   "production",
	}
	regist, err := etcd.Create(registrarOptions)
	if err != nil {
		return gerror.Wrap(err, "failed to create etcd registrar")
	}
	// v3.1.0 API：StartNode(name gen.Atom, options gen.NodeOptions)
	// Cookie 在 NetworkOptions.Cookie 中设置
	nodeOptions := gen.NodeOptions{
		Network: gen.NetworkOptions{
			Cookie:    a.cookie,
			Registrar: regist,
		},
	}

	node, err := ergo.StartNode(a.nodeName, nodeOptions)
	if err != nil {
		return gerror.Wrapf(err, "failed to start ergo node %s", a.nodeName)
	}

	a.node = node
	glog.Infof(ctx, "Ergo node started: %s", a.nodeName)
	return nil
}

// OnStop 停止Actor模块 - 停止节点
func (a *actorSystem) OnStop(ctx context.Context) error {
	if a.node != nil {
		// 停止节点
		a.node.Stop()
		glog.Infof(ctx, "Ergo node stopped: %s", a.nodeName)
	}
	return a.Module.OnStop(ctx)
}

// SpawnRegister创建新的Actor
func (a *actorSystem) SpawnRegister(name string, factory gen.ProcessFactory, options gen.ProcessOptions, args ...any) (gen.PID, error) {
	if a.node == nil {
		return gen.PID{}, fmt.Errorf("node not initialized")
	}

	pid, err := a.node.SpawnRegister(gen.Atom(name), factory, options, args...)
	if err != nil {
		return gen.PID{}, gerror.Wrap(err, "failed to spawn actor")
	}

	return pid, nil
}

func (a *actorSystem) Spawn(factory gen.ProcessFactory, options gen.ProcessOptions, args ...any) (gen.PID, error) {
	if a.node == nil {
		return gen.PID{}, gerror.New("node not initialized")
	}

	pid, err := a.node.Spawn(factory, options, args...)
	if err != nil {
		return gen.PID{}, gerror.Wrap(err, "failed to spawn actor ")
	}

	return pid, nil
}

// Send 发送消息（异步）
func (a *actorSystem) Send(pid any, message any) error {
	if a.node == nil {
		return fmt.Errorf("node not initialized")
	}

	return a.node.Send(pid, message)
}

// GetNode 获取节点实例
func (a *actorSystem) GetNode() gen.Node {
	return a.node
}

func (a *actorSystem) GetRemoteNode(name string) (gen.RemoteNode, error) {

	return a.node.Network().GetNode(gen.Atom(name))
}

// GetNodeName 获取节点名称
func (a *actorSystem) GetNodeName() string {
	return string(a.nodeName)
}

func (a *actorSystem) StopActor(pid gen.PID) error {
	if a.node == nil {
		return fmt.Errorf("node not initialized")
	}

	return a.node.Kill(pid)
}
