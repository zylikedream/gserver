package chat

import (
	"context"

	"gserver/core/gxypgx"

	"github.com/gogf/gf/v2/os/glog"
)

func InitChatSchema(ctx context.Context) {
	if err := gxypgx.DB().AutoMigrate(&ChatPrivateMessage{}); err != nil {
		glog.Fatal(ctx, err)
	}
	glog.Info(ctx, "[schema] chat tables migrated successfully")
}
