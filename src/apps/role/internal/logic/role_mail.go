package logic

import (
	"context"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxypgx"
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
	roleID := r.RoleID

	// 1. 加载元数据
	if err := loadModuleState(ctx, roleID, &r.meta); err != nil {
		return err
	}
	r.meta.SetRoleID(roleID)

	// 2. 加载玩家邮件列表（不含已删除）
	var mails []MailEntry
	if err := gxypgx.DB().WithContext(ctx).
		Where("role_id = ? AND is_deleted = false", roleID).
		Order("id DESC").
		Find(&mails).Error; err != nil {
		return err
	}
	r.mailCache = mails

	// 3. 清理过期邮件
	r.cleanExpired(ctx)

	// 4. 展开全服邮件
	if err := r.expandSysMail(ctx); err != nil {
		gxylog.Warn(ctx, "expand sys mail failed", gxylog.Err(err))
	}

	return nil
}

func (r *RoleMail) AfterLogin(ctx context.Context) {
	if err := r.expandSysMail(ctx); err != nil {
		gxylog.Warn(ctx, "after login expand sys mail failed", gxylog.Err(err))
	}
}

func (r *RoleMail) cleanExpired(ctx context.Context) {
	now := time.Now().Unix()
	var expiredIDs []int64
	var kept []MailEntry
	for _, m := range r.mailCache {
		if m.ExpireAt > 0 && m.ExpireAt < now {
			expiredIDs = append(expiredIDs, m.ID)
		} else {
			kept = append(kept, m)
		}
	}
	if len(expiredIDs) > 0 {
		gxypgx.DB().WithContext(ctx).
			Model(&MailEntry{}).
			Where("id IN ?", expiredIDs).
			Update("is_deleted", true)
	}
	r.mailCache = kept
}

func (r *RoleMail) expandSysMail(ctx context.Context) error {
	var maxID int64
	if err := gxypgx.DB().WithContext(ctx).
		Model(&SysMailItem{}).
		Select("COALESCE(MAX(id), 0)").
		Scan(&maxID).Error; err != nil {
		return err
	}

	if r.meta.LastExpandSysMail >= maxID {
		return nil
	}

	var sysMails []SysMailItem
	if err := gxypgx.DB().WithContext(ctx).
		Where("id > ?", r.meta.LastExpandSysMail).
		Find(&sysMails).Error; err != nil {
		return err
	}

	for _, sm := range sysMails {
		entry := MailEntry{
			RoleID:      r.RoleID,
			Title:       sm.Title,
			Content:     sm.Content,
			Attachments: sm.Attachments,
			SendAt:      sm.CreateAt,
			ExpireAt:    sm.ExpireAt,
			IsSysMail:   true,
		}
		r.meta.MaxID++
		entry.ID = r.meta.MaxID

		if err := gxypgx.DB().WithContext(ctx).Create(&entry).Error; err != nil {
			return err
		}
		r.mailCache = append(r.mailCache, entry)
	}

	r.meta.LastExpandSysMail = maxID
	r.meta.MarkDirty()
	return nil
}

func (r *RoleMail) OnCreate(ctx context.Context) {}

func (r *RoleMail) OnModStop(ctx context.Context) error { return nil }
