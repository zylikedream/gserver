package gxymodule

import (
	"context"
	"gserver/core/gxyutil"

	"gserver/core/gxylog"
)

type IModule interface {
	GetModName() string
	GetModID() string
	OnModInit(ctx context.Context) error
	OnModStart(ctx context.Context) error
	OnModStartAfter(ctx context.Context) error // 如果依赖与其他module,就实现这个方法
	OnModStop(ctx context.Context) error
	OnModStopBefore(ctx context.Context) error
	BaseModule() *ModuleBase
}

// 必须继承ModuleBase
type ModuleBase struct {
	name   string
	self   IModule
	parent IModule
	childs []IModule
}

func (m *ModuleBase) SelfMod() IModule {
	return m.self
}

func (m *ModuleBase) SetSelfMod(self IModule) {
	m.self = self
}

func (m *ModuleBase) GetModID() string {
	return m.GetModName()
}

func (m *ModuleBase) GetModName() string {
	return m.name
}

func (m *ModuleBase) OnModInit(ctx context.Context) error {
	return nil
}

func (m *ModuleBase) OnModStart(ctx context.Context) error {
	return nil
}

func (m *ModuleBase) OnModStartAfter(ctx context.Context) error {
	return nil
}

func (m *ModuleBase) OnModStop(ctx context.Context) error {
	return nil
}

func (m *ModuleBase) OnModStopBefore(ctx context.Context) error {
	return nil
}

func (m *ModuleBase) GetModule(id string) IModule {
	for _, child := range m.childs {
		if child.GetModName() == id {
			return child
		}
	}
	return nil
}

func (m *ModuleBase) AddModule(ctx context.Context, mod IModule) error {
	base := mod.BaseModule()
	base.name = gxyutil.GetObjectName(mod)
	base.self = mod
	base.parent = m.self
	if err := mod.OnModInit(ctx); err != nil {
		return err
	}
	m.childs = append(m.childs, mod)
	return nil
}

func (m *ModuleBase) StartModule(ctx context.Context) error {
	if m.self != nil {
		if err := m.self.OnModStart(ctx); err != nil {
			return err
		}
	}
	//启动子孙
	for _, child := range m.childs {
		mod := child.BaseModule()
		if err := mod.StartModule(ctx); err != nil {
			return err
		}
	}
	for _, child := range m.childs {
		if err := child.OnModStartAfter(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (m *ModuleBase) StopModule(ctx context.Context) error {
	//释放子孙
	for i := len(m.childs) - 1; i >= 0; i-- {
		mod := m.childs[i].BaseModule()
		if err := mod.StopModule(ctx); err != nil {
			gxylog.Error(ctx, "stop child mod failed", gxylog.Str("mod", mod.GetModName()), gxylog.Err(err))
		}
	}
	if m.self != nil {
		if err := m.self.OnModStop(ctx); err != nil {
			return err
		}
	}
	m.self = nil
	m.parent = nil
	m.childs = nil
	return nil
}

func (m *ModuleBase) GetParent() IModule {
	return m.parent
}

func (m *ModuleBase) Modules() []IModule {
	return m.childs
}

func (m *ModuleBase) BaseModule() *ModuleBase {
	return m
}
