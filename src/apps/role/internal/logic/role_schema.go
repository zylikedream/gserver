package logic

import (
	"context"

	"gserver/core/gxypgx"

	"github.com/gogf/gf/v2/os/glog"
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
		&RoleMainTaskState{},
	); err != nil {
		glog.Fatal(ctx, err)
	}

	glog.Info(ctx, "[schema] all role tables migrated successfully")
}
