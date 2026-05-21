package logic

import (
	"context"

	"gserver/core/gxylog"
	"gserver/core/gxypgx"
)

func InitRoleSchema(ctx context.Context) {
	db := gxypgx.DB()

	if err := db.AutoMigrate(
		&RoleAccount{},
		&RoleBasicState{},
		&RoleBagState{},
		&RoleExtraPersistState{},
		&RolePublicState{},
		&RoleFlowerState{},
		&RolePlotState{},
		&RoleStealState{},
		&RoleMainTaskState{},
		&RoleResidentOrderState{},
		&StealRecord{},
		&RoleChatState{},
		&MailEntry{},
		&RoleMailMeta{},
		&SysMailItem{},
	); err != nil {
		gxylog.Fatal(ctx, "role schema migration failed", gxylog.Err(err))
	}

	gxylog.Info(ctx, "[schema] all role tables migrated successfully")
}
