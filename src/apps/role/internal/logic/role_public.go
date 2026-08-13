package logic

import (
	"context"
	"fmt"
	"gserver/core/gxylog"
	"gserver/core/gxyredis"
	"gserver/protocol/pb"
	"gserver/src/apps/api"
	"gserver/src/pkg/deps"
	"time"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/redis/go-redis/v9"

	"gorm.io/gorm"
)

const (
	RolePublicCacheExpire = time.Hour
)

type RolePublicState struct {
	RolePersistState
	api.RolePublicData
}

func (RolePublicState) TableName() string { return "role_public" }

func (r *RolePublicState) GetIndexes() []string {
	return []string{"update_at"}
}

type RolePublic struct {
	RoleModule
	RolePublicState
}

func (r *RolePublic) PersistState() IPersistState {
	return &r.RolePublicState
}

func (r *RolePublic) UpdateRolePublic(ctx context.Context) {
	role := r.Role
	r.Name = role.Basic.RoleName
	r.Head = role.Basic.Head
	r.CreateTime = role.Basic.CreateTm
	r.Level = role.Basic.Level
	r.LastLoginAt = role.Basic.LoginTm
	r.MarkDirty()
}

func (r *RolePublic) GetRolePublic(ctx context.Context) *pb.PRolePublic {
	return PRolePublic(&r.RolePublicState)
}

func GetRolePublic(ctx context.Context, d deps.Deps, roleID int64) *pb.PRolePublic {
	rolePublic := doGetRolePublic(ctx, d, roleID)
	if rolePublic == nil {
		return nil
	}

	return PRolePublic(rolePublic)
}

func PRolePublic(rolePublic *RolePublicState) *pb.PRolePublic {
	return &pb.PRolePublic{
		RoleId:      rolePublic.RoleID,
		Name:        rolePublic.Name,
		Head:        rolePublic.Head,
		CreateTime:  rolePublic.CreateTime.Unix(),
		Level:       rolePublic.Level,
		LastLoginAt: rolePublic.LastLoginAt.Unix(),
		IsOnline:    rolePublic.IsOnline,
	}
}

func doGetRolePublic(ctx context.Context, d deps.Deps, roleID int64) *RolePublicState {
	rolePublic := GetRolePublicFromCache(ctx, d.Redis, roleID)
	if rolePublic != nil {
		return rolePublic
	}
	rolePublic = GetRolePublicFromDB(ctx, d.DB, roleID)
	// set to cache
	if rolePublic != nil {
		setRolePublicToCache(ctx, d.Redis, rolePublic)
		return rolePublic
	}
	return nil
}

func getRolePublicKey(roleID int64) string {
	return fmt.Sprintf("role_public:%d", roleID)
}

func GetRolePublicFromCache(ctx context.Context, cli gxyredis.Client, roleID int64) *RolePublicState {
	key := getRolePublicKey(roleID)
	rolePublic := &RolePublicState{}
	strPublic, err := cli.Get(ctx, key).Result()
	if err != nil {
		if err != redis.Nil {
			gxylog.Error(ctx, "get role public from cache failed", gxylog.Num("roleID", roleID), gxylog.Err(err))
		}
		return nil
	}
	if err := gjson.Unmarshal([]byte(strPublic), rolePublic); err != nil {
		gxylog.Error(ctx, "unmarshal role public from cache failed", gxylog.Num("roleID", roleID), gxylog.Err(err))
		return nil
	}
	return rolePublic
}

func setRolePublicToCache(ctx context.Context, cli gxyredis.Client, rolePublic *RolePublicState) {
	key := getRolePublicKey(rolePublic.RoleID)
	strPublic, err := gjson.EncodeString(rolePublic)
	if err != nil {
		gxylog.Error(ctx, "marshal role public to cache failed", gxylog.Num("roleID", rolePublic.RoleID), gxylog.Err(err))
		return
	}
	if err := cli.Set(ctx, key, strPublic, RolePublicCacheExpire).Err(); err != nil {
		gxylog.Error(ctx, "set role public to cache failed", gxylog.Num("roleID", rolePublic.RoleID), gxylog.Err(err))
	}
}

func GetRolePublicFromDB(ctx context.Context, db *gorm.DB, roleID int64) *RolePublicState {
	rolePublic := &RolePublicState{}
	if err := loadModuleState(ctx, db, roleID, rolePublic); err != nil {
		gxylog.Error(ctx, "load role public from db failed", gxylog.Num("roleID", roleID), gxylog.Err(err))
		return nil
	}
	return rolePublic
}
