package logic

import (
	"context"

	"gserver/core/gxypgx"

	"github.com/gogf/gf/v2/os/glog"
)

func InitFriendSchema(ctx context.Context) {
	if err := gxypgx.DB().AutoMigrate(&FriendData{}); err != nil {
		glog.Fatal(ctx, err)
	}
	glog.Info(ctx, "[schema] friend_data table migrated successfully")
}
