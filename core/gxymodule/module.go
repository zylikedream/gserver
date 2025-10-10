package gxymodule

import (
	"context"
	"gserver/util"

	"github.com/gogf/gf/v2/os/glog"
)

type IModule interface {
	GetName() string
	GetID() string
	OnInit(ctx context.Context) error
	OnStart(ctx context.Context) error
	OnStop(ctx context.Context) error
	BaseModule() *Module
}

// 必须继承Module
type Module struct {
	name   string
	self   IModule
	parent IModule
	childs map[string]IModule
}

func (m *Module) Self() IModule {
	return m.self
}

func (m *Module) SetSelf(self IModule) {
	m.self = self
}

func (m *Module) GetID() string {
	return m.GetName()
}

func (m *Module) GetName() string {
	return m.name
}

func (m *Module) OnInit(ctx context.Context) error {
	return nil
}

func (m *Module) OnStart(ctx context.Context) error {
	return nil
}

func (m *Module) OnStop(ctx context.Context) error {
	return nil
}

func (m *Module) GetModule(id string) IModule {
	mod := m.childs[id]
	return mod
}

func (m *Module) AddModule(ctx context.Context, mod IModule) error {
	base := mod.BaseModule()
	if m.childs == nil {
		m.childs = map[string]IModule{}
	}
	base.name = util.GetObjectName(mod)
	base.self = mod
	base.parent = m.self
	if err := mod.OnInit(ctx); err != nil {
		return err
	}
	m.childs[mod.GetID()] = mod
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	if err := m.self.OnStart(ctx); err != nil {
		return err
	}
	//启动子孙
	for _, child := range m.childs {
		mod := child.BaseModule()
		if err := mod.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	//释放子孙
	for _, child := range m.childs {
		mod := child.BaseModule()
		if err := mod.Stop(ctx); err != nil {
			glog.Errorf(ctx, "stop child mod %s err: %v", mod.GetName(), err)
		}
	}
	if err := m.self.OnStop(ctx); err != nil {
		return err
	}
	m.self = nil
	m.parent = nil
	m.childs = nil
	return nil
}

func (m *Module) StopModule(ctx context.Context, modName string) error {
	mod := m.GetModule(modName).BaseModule()
	if mod == nil {
		return nil
	}
	if err := mod.Stop(ctx); err != nil {
		return err
	}
	delete(m.childs, modName)
	return nil
}

func (m *Module) GetParent() IModule {
	return m.parent
}

func (m *Module) Modules() map[string]IModule {
	return m.childs
}

func (m *Module) BaseModule() *Module {
	return m
}
