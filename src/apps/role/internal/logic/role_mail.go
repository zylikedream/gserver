package logic

import (
	"context"

	"gserver/src/apps/role/internal/logic/bag"
)

// MailEntry 单封邮件（role_mail_items 表）
type MailEntry struct {
	ID          int64      `gorm:"primaryKey"`
	RoleID      int64      `gorm:"column:role_id;index:idx_mail_role_id"`
	Title       string     `gorm:"column:title"`
	Summary     string     `gorm:"column:summary"`
	Content     string     `gorm:"column:content"`
	Attachments []bag.Good `gorm:"column:attachments;type:jsonb;serializer:json"`
	SendAt      int64      `gorm:"column:send_at"`
	ExpireAt    int64      `gorm:"column:expire_at"`
	IsRead      bool       `gorm:"column:is_read"`
	IsClaimed   bool       `gorm:"column:is_claimed"`
	IsSysMail   bool       `gorm:"column:is_sys_mail"`
	IsDeleted   bool       `gorm:"column:is_deleted"`
}

func (MailEntry) TableName() string { return "role_mail_items" }

// RoleMailMeta 角色邮件元数据
type RoleMailMeta struct {
	RolePersistState
	MaxID             int64 `gorm:"column:max_id"`
	LastExpandSysMail int64 `gorm:"column:last_expand_sys_mail_id"`
}

func (RoleMailMeta) TableName() string { return "role_mail_meta" }

func (r *RoleMailMeta) GetIndexes() []string {
	return []string{"update_at"}
}

// SysMailItem 全服邮件
type SysMailItem struct {
	ID          int64      `gorm:"primaryKey;autoIncrement"`
	Title       string     `gorm:"column:title"`
	Content     string     `gorm:"column:content"`
	Attachments []bag.Good `gorm:"column:attachments;type:jsonb;serializer:json"`
	ExpireAt    int64      `gorm:"column:expire_at"`
	CreateAt    int64      `gorm:"column:create_at"`
}

func (SysMailItem) TableName() string { return "sys_mail" }

// RoleMail 模块
type RoleMail struct {
	RoleModule
	mailCache []MailEntry
	meta      RoleMailMeta
}

var _ IRoleModule = (*RoleMail)(nil)

func (r *RoleMail) PersistState() IPersistState {
	return &r.meta
}

func (r *RoleMail) OnModInit(ctx context.Context) error {
	return nil // Task 3 中实现
}

func (r *RoleMail) AfterLogin(ctx context.Context) {
	// Task 3 中实现
}

func (r *RoleMail) OnCreate(ctx context.Context) {}

func (r *RoleMail) OnModStop(ctx context.Context) error { return nil }
