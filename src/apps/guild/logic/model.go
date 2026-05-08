package logic

import (
	"time"
)

// Guild 公会主表，Members/ApplyList/Logs 存储为 JSONB 列
type Guild struct {
	ID           int64         `gorm:"column:id;primaryKey;autoIncrement"`
	Name         string        `gorm:"column:name;uniqueIndex;size:32"`
	Level        int32         `gorm:"column:level"`
	Icon         string        `gorm:"column:icon;size:256"`
	Declaration  string        `gorm:"column:declaration;size:200"`
	Announcement string        `gorm:"column:announcement;size:500"`
	NeedApproval bool          `gorm:"column:need_approval"`
	MemberCount  int32         `gorm:"column:member_count"`
	LeaderID     int64         `gorm:"column:leader_id"`
	Members      []*GuildMember `gorm:"type:jsonb;serializer:json"`
	ApplyList    []*GuildApply  `gorm:"type:jsonb;serializer:json"`
	Logs         []*GuildLog    `gorm:"type:jsonb;serializer:json"`
	CreatedAt    time.Time     `gorm:"column:created_at"`
	UpdatedAt    time.Time     `gorm:"column:updated_at"`
	Version      int64         `gorm:"column:version"`
}

func (Guild) TableName() string { return "guild" }

// GuildMember — JSONB 嵌入，不存 PRolePublic，由 GetRolePublic 动态填充
// Position: 1=会长 2=副会长 3=成员 (gamecfg.GardenEGuildPosition_LEADER/VICE_LEADER/MEMBER)
type GuildMember struct {
	RoleID   int64 `json:"role_id"`
	Position int32 `json:"position"`
	JoinedAt int64 `json:"joined_at"`
}

// GuildApply — JSONB 嵌入
type GuildApply struct {
	ID        int64     `json:"id"`
	RoleID    int64     `json:"role_id"`
	Status    int32     `json:"status"` // 0=待处理 1=同意 2=拒绝
	CreatedAt time.Time `json:"created_at"`
	ExpireAt  time.Time `json:"expire_at"`
}

// GuildLog — JSONB 嵌入（最多 100 条）
type GuildLog struct {
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// GuildRoleState 映射 role_guild 表，供 guild actor 原子操作（独立表不走 JSONB）
type GuildRoleState struct {
	RoleID  int64 `gorm:"column:role_id;primaryKey"`
	GuildID int64 `gorm:"column:guild_id"`
}

func (GuildRoleState) TableName() string { return "role_guild" }
