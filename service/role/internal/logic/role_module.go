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
	gxymodule.Module `bson:"-"`
	Role             *RoleMain `bson:"-" hash:"-"`
}

func (r *RoleModule) GetRole() *RoleMain {
	return r.Role
}

func (r *RoleModule) AfterInit(ctx context.Context) error {
	r.Role = r.GetParent().(*RoleMain)
	return nil
}
