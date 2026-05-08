package logic

import (
	"context"

	"gserver/core/gxypgx"

	"github.com/gogf/gf/v2/os/glog"
)

func InitGuildSchema(ctx context.Context) {
	if err := gxypgx.DB().AutoMigrate(
		&Guild{},
	); err != nil {
		glog.Fatal(ctx, err)
	}
	glog.Info(ctx, "[schema] guild table migrated successfully")
}
