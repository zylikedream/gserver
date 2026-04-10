package lib

import (
	"context"
	"gserver/core/gxyactor"

	"github.com/gogf/gf/v2/os/glog"
)

func NotifyRoleMessageOnline(ctx context.Context, roleID int64, message any) bool {
	return notifyRoleMessage(ctx, roleID, message, false)
}

func notifyRoleMessage(ctx context.Context, roleID int64, message any, anywhere bool) bool {
	pid, _ := GetRoleGrain(roleID, anywhere)
	if pid == nil {
		glog.Debugf(ctx, "notify failed, role grain not found, roleID:%d message:%v", roleID, message)
		return false
	}

	if err := gxyactor.Send(pid, message); err != nil {
		glog.Errorf(ctx, "notify failed, roleID:%d message:%v err:%+v", roleID, message, err)
		return false
	}
	return true
}
