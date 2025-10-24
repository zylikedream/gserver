package logic

import (
	"context"
	"gserver/core/gxymodule"
	"gserver/util"
	"time"

	"github.com/gogf/gf/v2/text/gstr"
)

type IRoleModule interface {
	gxymodule.IModule
	SetRole(role *RoleMain)
	OnCreate(ctx context.Context)
	PersistState() IPersistState // must return pointer
}

type IPersistState interface {
	SetRoleID(roleID int64)
	GetVersion() int64
	SetVersion(version int64)
	GetUpdateAt() time.Time
	SetUpdateAt(updateAt time.Time)
}

type RolePersistState struct {
	RoleID   int64     `bson:"role_id"`
	UpdateAt time.Time `bson:"update_at"`
	Version  int64     `bson:"version"`
}

func (r *RolePersistState) SetRoleID(roleID int64) {
	r.RoleID = roleID
}

func (r *RolePersistState) GetVersion() int64 {
	return r.Version
}

func (r *RolePersistState) SetVersion(version int64) {
	r.Version = version
}

func (r *RolePersistState) GetUpdateAt() time.Time {
	return r.UpdateAt
}

func (r *RolePersistState) SetUpdateAt(updateAt time.Time) {
	r.UpdateAt = updateAt
}

func getColName(mod IPersistState) string {
	return gstr.CaseSnake(util.GetObjectName(mod))
}

type RoleModule struct {
	gxymodule.ModuleBase `bson:"-"`
	RoleID               int64
	Role                 *RoleMain `bson:"-" hash:"-"`
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
