package logic

import (
	"context"

	"gserver/core/gxylog"

	"gorm.io/gorm"
)

func InitRoleSchema(ctx context.Context, db *gorm.DB) {

	if err := db.Exec("CREATE SEQUENCE IF NOT EXISTS mail_global_id_seq").Error; err != nil {
		gxylog.Fatal(ctx, "mail sequence migration failed", gxylog.Err(err))
	}

	if err := db.AutoMigrate(
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
		&PersonalMailItem{},
		&RoleMailState{},
		&SysMailItem{},
	); err != nil {
		gxylog.Fatal(ctx, "role schema migration failed", gxylog.Err(err))
	}

	gxylog.Info(ctx, "[schema] all role tables migrated successfully")
}
