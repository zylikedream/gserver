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
	RoleID           int64     `bson:"role_id"`
	Role             *RoleMain `bson:"-" hash:"-"`
}

func (r *RoleModule) GetRole() *RoleMain {
	return r.Role
}

func (r *RoleModule) AfterInit(ctx context.Context) error {
	r.Role = r.GetParent().(*RoleMain)
	r.RoleID = r.Role.RoleID
	return nil
}
