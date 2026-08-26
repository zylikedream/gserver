package logic

import (
	"context"

	"gorm.io/gorm"
)

func InitAccountSchema(ctx context.Context, db *gorm.DB) error {
	// role_id 生成依赖 PG sequence:持久、原子、多实例安全(替代 Redis INCR)。
	if err := db.WithContext(ctx).Exec("CREATE SEQUENCE IF NOT EXISTS uid_role_seq").Error; err != nil {
		return err
	}
	// 起始值对齐现有数据,只在已有账号时执行;GREATEST 保证不回退
	// (sequence 已推进或并发启动时保持较大值,幂等自愈)。
	var maxRoleID int64
	if err := db.WithContext(ctx).
		Raw("SELECT COALESCE(MAX(role_id),0) FROM account").
		Scan(&maxRoleID).Error; err != nil {
		return err
	}
	if maxRoleID > 0 {
		if err := db.WithContext(ctx).
			Exec("SELECT setval('uid_role_seq', GREATEST(?, (SELECT last_value FROM uid_role_seq)))", maxRoleID).
			Error; err != nil {
			return err
		}
	}
	return db.WithContext(ctx).AutoMigrate(&Account{}, &AccountIdentity{})
}
