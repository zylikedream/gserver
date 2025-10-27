package logic

import (
	"context"
	"fmt"
	"gserver/core/gxyredis"
	"gserver/protocol/pb"
	"time"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/redis/go-redis/v9"
)

const (
	RolePublicCacheExpire = time.Hour
)

type RolePublicState struct {
	RolePersistState `bson:"inline"`
	Name             string    `bson:"name"`
	Head             string    `bson:"head"`
	CreateTime       time.Time `bson:"create_time"`
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
}

func GetRolePublic(ctx context.Context, roleID int64) *pb.PRolePublic {
	rolePublic := doGetRolePublic(ctx, roleID)
	if rolePublic == nil {
		return nil
	}
	return &pb.PRolePublic{
		RoleId:     rolePublic.RoleID,
		Name:       rolePublic.Name,
		Head:       rolePublic.Head,
		CreateTime: rolePublic.CreateTime.Unix(),
	}
}

func doGetRolePublic(ctx context.Context, roleID int64) *RolePublicState {
	rolePublic := GetRolePublicFromCache(ctx, roleID)
	if rolePublic != nil {
		return rolePublic
	}
	rolePublic = GetRolePublicFromDB(ctx, roleID)
	// set to cache
	if rolePublic != nil {
		setRolePublicToCache(ctx, rolePublic)
		return rolePublic
	}
	return nil
}

func getRolePublicKey(roleID int64) string {
	return fmt.Sprintf("role_public:%d", roleID)
}

func GetRolePublicFromCache(ctx context.Context, roleID int64) *RolePublicState {
	key := getRolePublicKey(roleID)
	rolePublic := &RolePublicState{}
	strPublic, err := gxyredis.GetRedis().Get(ctx, key).Result()
	if err != nil {
		if err != redis.Nil {
			glog.Errorf(ctx, "get role public from cache failed, roleID: %d, err: %v", roleID, err)
		}
		return nil
	}
	if err := gjson.Unmarshal([]byte(strPublic), rolePublic); err != nil {
		glog.Errorf(ctx, "unmarshal role public from cache failed, roleID: %d, err: %v", roleID, err)
		return nil
	}
	return rolePublic
}

func setRolePublicToCache(ctx context.Context, rolePublic *RolePublicState) {
	key := getRolePublicKey(rolePublic.RoleID)
	strPublic, err := gjson.EncodeString(rolePublic)
	if err != nil {
		glog.Errorf(ctx, "marshal role public to cache failed, roleID: %d, err: %v", rolePublic.RoleID, err)
		return
	}
	if err := gxyredis.GetRedis().Set(ctx, key, strPublic, RolePublicCacheExpire).Err(); err != nil {
		glog.Errorf(ctx, "set role public to cache failed, roleID: %d, err: %v", rolePublic.RoleID, err)
	}
}

func GetRolePublicFromDB(ctx context.Context, roleID int64) *RolePublicState {
	rolePublic := &RolePublicState{}
	if err := loadModuleState(ctx, roleID, rolePublic); err != nil {
		glog.Errorf(ctx, "load role public from db failed, roleID: %d, err: %v", roleID, err)
		return nil
	}
	return rolePublic
}
