package logic

import (
	"context"

	"gorm.io/gorm"

	"gserver/core/gxylog"
)

func InitFriendSchema(ctx context.Context, db *gorm.DB) {
	if err := db.AutoMigrate(&FriendData{}, &FriendRelation{}); err != nil {
		gxylog.Fatal(ctx, "friend schema migration failed", gxylog.Err(err))
	}
	gxylog.Info(ctx, "[schema] friend tables migrated successfully")
}
