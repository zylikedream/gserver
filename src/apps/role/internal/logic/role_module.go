package logic

import (
	"context"
	"gserver/core/gxymodule"
	"gserver/core/gxypgx"
	"gserver/core/gxyredis"
	"gserver/src/pkg/deps"
	"gserver/src/pkg/gameconfig"
	"time"

	"gorm.io/gorm"
)

type IRoleModule interface {
	gxymodule.IModule
	SetRole(role *RoleMain)
	OnCreate(ctx context.Context)
	AfterLogin(ctx context.Context)
	BeforeLogout(ctx context.Context)
	PersistState() IPersistState // must return pointer
}

type IPersistState interface {
	SetRoleID(roleID int64)
	GetUpdateAt() time.Time
	SetUpdateAt(updateAt time.Time)
	GetIndexes() []string
	MarkDirty()
	IsDirty() bool
	ClearDirty()
	GetVersion() int64
	SetVersion(v int64)
}

type RolePersistState struct {
	RoleID   int64     `gorm:"column:role_id;primaryKey"`
	UpdateAt time.Time `gorm:"column:update_at;autoUpdateTime"`
	Version  int64     `gorm:"column:version"`
	dirty    bool
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

func (r *RolePersistState) MarkDirty()         { r.dirty = true }
func (r *RolePersistState) IsDirty() bool      { return r.dirty }
func (r *RolePersistState) ClearDirty()        { r.dirty = false }
func (r *RolePersistState) GetVersion() int64  { return r.Version }
func (r *RolePersistState) SetVersion(v int64) { r.Version = v }

type RoleModule struct {
	gxymodule.ModuleBase
	RoleID int64
	Role   *RoleMain
}

// DB 返回数据库连接:优先注入的 deps,未注入时回退全局单例。
// 生产路径由组装根注入;测试注入 go-sqlmock 后走 mock。
func (r *RoleModule) DB() *gorm.DB {
	if r.Role != nil && r.Role.deps.DB != nil {
		return r.Role.deps.DB
	}
	return gxypgx.DB()
}

// Redis 返回缓存客户端,语义同 DB。
func (r *RoleModule) Redis() gxyredis.Client {
	if r.Role != nil && r.Role.deps.Redis != nil {
		return r.Role.deps.Redis
	}
	return gxyredis.Redis()
}

// Cfg 返回游戏配表,语义同 DB。
func (r *RoleModule) Cfg() *gameconfig.GameConfig {
	if r.Role != nil && r.Role.deps.Cfg != nil {
		return r.Role.deps.Cfg
	}
	return gameconfig.Get()
}

// Deps 返回完整依赖集合(未注入的字段回退全局单例)。
// 供无接收者的自由函数/外部系统入口(如 SendMail)使用。
func (r *RoleModule) Deps() deps.Deps {
	d := deps.Deps{}
	if r.Role != nil {
		d = r.Role.deps
	}
	if d.DB == nil {
		d.DB = gxypgx.DB()
	}
	if d.Redis == nil {
		d.Redis = gxyredis.Redis()
	}
	if d.Cfg == nil {
		d.Cfg = gameconfig.Get()
	}
	return d
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

func (r *RoleModule) AfterLogin(ctx context.Context) {
}

func (r *RoleModule) BeforeLogout(ctx context.Context) {
}
