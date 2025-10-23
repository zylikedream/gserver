package logic

import (
	"context"
	"gserver/core/gxymodule"
)

type IRoleModule interface {
	gxymodule.IModule
	SetRole(role *RoleMain)
	OnCreate(ctx context.Context)
	PersistState() IPersistState // must return pointer
}

type IPersistState interface {
	SetRoleID(roleID int64)
}

type RolePersistState struct {
	RoleID int64 `bson:"role_id"`
}

func (r *RolePersistState) SetRoleID(roleID int64) {
	r.RoleID = roleID
}

type RoleModule struct {
	gxymodule.ModuleBase `bson:"-"`
	Role                 *RoleMain `bson:"-" hash:"-"`
}

func (r *RoleModule) SetRole(role *RoleMain) {
	r.Role = role
}

func (r *RoleModule) OnCreate(ctx context.Context) {
}

func (r *RoleModule) PersistState() IPersistState {
	return nil
}
