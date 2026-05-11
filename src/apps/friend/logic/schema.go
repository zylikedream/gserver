package logic

import (
	"context"

	"gserver/core/gxylog"
	"gserver/core/gxypgx"
)

func InitFriendSchema(ctx context.Context) {
	if err := gxypgx.DB().AutoMigrate(&FriendData{}, &FriendRelation{}); err != nil {
		gxylog.Fatal(ctx, "friend schema migration failed", gxylog.Err(err))
	}
	gxylog.Info(ctx, "[schema] friend tables migrated successfully")
}
