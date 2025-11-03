package logic

import (
	"context"
	"gserver/core/gxymodule"
	"gserver/util"
	"time"

	"github.com/gogf/gf/v2/text/gstr"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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
	GetIndexes() []mongo.IndexModel
}

type RolePersistState struct {
	RoleID   int64     `bson:"role_id"`
	UpdateAt time.Time `bson:"update_at" hash:"-"`
	Version  int64     `bson:"version" hash:"-"`
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

func (r *RolePersistState) GetIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "role_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
	}
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
