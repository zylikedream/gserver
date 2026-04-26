package logic

import (
	"context"
	"gserver/core/gxymodule"
	"time"
)

type IRoleModule interface {
	gxymodule.IModule
	SetRole(role *RoleMain)
	OnCreate(ctx context.Context)
	PersistState() IPersistState // must return pointer
}

type IPersistState interface {
	SetRoleID(roleID int64)
	GetUpdateAt() time.Time
	SetUpdateAt(updateAt time.Time)
	GetIndexes() []string
}

type RolePersistState struct {
	RoleID   int64     `gorm:"column:role_id;primaryKey"`
	UpdateAt time.Time `gorm:"column:update_at;autoUpdateTime"`
}

func (r *RolePersistState) SetRoleID(roleID int64) {
	r.RoleID = roleID
}

func (r *RolePersistState) GetUpdateAt() time.Time {
	return r.UpdateAt
}

func (r *RolePersistState) SetUpdateAt(updateAt time.Time) {
	r.UpdateAt = updateAt
}

func (r *RolePersistState) GetIndexes() []string {
	return []string{"update_at"}
}

type RoleModule struct {
	gxymodule.ModuleBase
	RoleID int64
	Role   *RoleMain
}

func (r *RoleModule) SetRole(role *RoleMain) {
	r.RoleID = role.RoleID
	r.Role = role
}

func (r *RoleModule) OnCreate(ctx context.Context) {
}

func (r *RoleModule) PersistState() IPersistState {
	return nil
}
