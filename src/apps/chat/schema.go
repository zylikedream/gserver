package chat

import (
	"context"

	"gserver/core/gxylog"
	"gserver/core/gxypgx"
)

func InitChatSchema(ctx context.Context) {
	if err := gxypgx.DB().AutoMigrate(&ChatPrivateMessage{}, &ChatSystemMessage{}); err != nil {
		gxylog.Fatal(ctx, "chat schema migration failed", gxylog.Err(err))
	}
	gxylog.Info(ctx, "[schema] chat tables migrated successfully")
}
