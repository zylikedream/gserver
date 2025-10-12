package logic

import (
	"context"
	"gserver/core/gxymodule"
)

type IRoleModule interface {
	gxymodule.IModule
	SetRole(role *RoleMain)
	OnCreate(ctx context.Context)
}

type RoleModule struct {
	gxymodule.Module `bson:"-"`
	RoleID           int64     `bson:"role_id"`
	Role             *RoleMain `bson:"-" hash:"-"`
}

func (r *RoleModule) SetRole(role *RoleMain) {
	r.Role = role
	r.RoleID = role.RoleID
}

func (r *RoleModule) OnCreate(ctx context.Context) {
}
