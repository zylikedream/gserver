package logic

import (
	"context"

	"gserver/core/gxylog"

	"gorm.io/gorm"
)

func InitGuildSchema(ctx context.Context, db *gorm.DB) {
	if err := db.AutoMigrate(
		&Guild{},
		&GuildRoleState{},
	); err != nil {
		gxylog.Fatal(ctx, "guild schema migration failed", gxylog.Err(err))
	}
	gxylog.Info(ctx, "[schema] guild table migrated successfully")
}
