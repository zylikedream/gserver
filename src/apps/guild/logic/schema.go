package logic

import (
	"context"

	"gserver/core/gxylog"
	"gserver/core/gxypgx"
)

func InitGuildSchema(ctx context.Context) {
	if err := gxypgx.DB().AutoMigrate(
		&Guild{},
		&GuildRoleState{},
	); err != nil {
		gxylog.Fatal(ctx, "guild schema migration failed", gxylog.Err(err))
	}
	gxylog.Info(ctx, "[schema] guild table migrated successfully")
}
