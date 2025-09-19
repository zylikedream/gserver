package gxyactor

import (
	"context"
	"fmt"

	"gserver/core/gxymodule"

	"ergo.services/ergo"
	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/os/glog"
)

// ActorSystem 基础Actor模块
type ActorSystem struct {
	gxymodule.Module
	node     gen.Node
	nodeName gen.Atom
	cookie   string
}

// NewActorSystem创建基础Actor模块
func NewActorSystem(nodeName gen.Atom, cookie string) *ActorSystem {
	return &ActorSystem{
		Module:   gxymodule.Module{},
		nodeName: nodeName,
		cookie:   cookie,
	}
}

// OnInit Actor模块初始化 - 启动节点
func (a *ActorSystem) OnInit(ctx context.Context) error {
	if err := a.Module.OnInit(ctx); err != nil {
		return err
	}
	// v3.1.0 API：StartNode(name gen.Atom, options gen.NodeOptions)
	// Cookie 在 NetworkOptions.Cookie 中设置
	nodeOptions := gen.NodeOptions{
		Network: gen.NetworkOptions{
			Cookie: a.cookie,
		},
	}

	node, err := ergo.StartNode(a.nodeName, nodeOptions)
	if err != nil {
		return fmt.Errorf("failed to start ergo node %s: %w", a.nodeName, err)
	}

	a.node = node
	glog.Infof(ctx, "Ergo node started: %s", a.nodeName)
	return nil
}

// OnStop 停止Actor模块 - 停止节点
func (a *ActorSystem) OnStop(ctx context.Context) error {
	if a.node != nil {
		// 停止节点
		a.node.Stop()
		glog.Infof(ctx, "Ergo node stopped: %s", a.nodeName)
	}
	return a.Module.OnStop(ctx)
}

// SpawnRegister创建新的Actor
func (a *ActorSystem) SpawnRegister(name string, factory gen.ProcessFactory, options gen.ProcessOptions, args ...any) (gen.PID, error) {
	if a.node == nil {
		return gen.PID{}, fmt.Errorf("node not initialized")
	}

	pid, err := a.node.SpawnRegister(gen.Atom(name), factory, options, args)
	if err != nil {
		return gen.PID{}, fmt.Errorf("failed to spawn actor %s: %w", name, err)
	}

	return pid, nil
}

func (a *ActorSystem) Spawn(factory gen.ProcessFactory, options gen.ProcessOptions, args ...any) (gen.PID, error) {
	if a.node == nil {
		return gen.PID{}, fmt.Errorf("node not initialized")
	}

	pid, err := a.node.Spawn(factory, options, args)
	if err != nil {
		return gen.PID{}, fmt.Errorf("failed to spawn actor : %w", err)
	}

	return pid, nil
}

// Send 发送消息（异步）
func (a *ActorSystem) Send(pid gen.PID, message any) error {
	if a.node == nil {
		return fmt.Errorf("node not initialized")
	}

	return a.node.Send(pid, message)
}

// GetNode 获取节点实例
func (a *ActorSystem) GetNode() gen.Node {
	return a.node
}

// GetNodeName 获取节点名称
func (a *ActorSystem) GetNodeName() string {
	return string(a.nodeName)
}

func (a *ActorSystem) StopActor(pid gen.PID) error {
	if a.node == nil {
		return fmt.Errorf("node not initialized")
	}

	return a.node.Kill(pid)
}
