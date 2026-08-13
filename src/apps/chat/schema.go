package chat

import (
	"context"

	"gserver/core/gxylog"

	"gorm.io/gorm"
)

func InitChatSchema(ctx context.Context, db *gorm.DB) {
	if err := db.AutoMigrate(&ChatPrivateMessage{}, &ChatSystemMessage{}); err != nil {
		gxylog.Fatal(ctx, "chat schema migration failed", gxylog.Err(err))
	}
	gxylog.Info(ctx, "[schema] chat tables migrated successfully")
}
