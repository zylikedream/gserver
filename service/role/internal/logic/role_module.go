package logic

import (
	"context"
	"gserver/core/gxymodule"
)

type IRoleModule interface {
	gxymodule.IModule
	AfterInit(ctx context.Context) error
}

type RoleModule struct {
	gxymodule.Module
	Role *RoleMain
}

func (r *RoleModule) GetRole() *RoleMain {
	return r.GetParent().(*RoleMain)
}

func (r *RoleModule) AfterInit(ctx context.Context) error {
	return nil
}
