package push

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxyhttp"
	"gserver/service"

	"github.com/gogf/gf/v2/os/glog"
)

type pushService struct {
	gxyhttp.HttpService
}

var pushSvc = newPushService()

func newPushService() *pushService {
	return &pushService{}
}

func PushService() *pushService {
	return pushSvc
}

func (s *pushService) Name() string {
	return service.SOCIAL_SERVICE
}

func (s *pushService) OnModStart(ctx context.Context) error {

	return nil
}

func (s *pushService) OnModInit(ctx context.Context) error {
	return nil
}

func (s *pushService) NotifyRoleMessageOnline(ctx context.Context, roleID int64, message any) bool {
	return s.NotifyRoleMessage(ctx, roleID, message, false)
}

func (s *pushService) NotifyRoleMessageAnywhere(ctx context.Context, roleID int64, message any) bool {
	return s.NotifyRoleMessage(ctx, roleID, message, true)
}

func (s *pushService) NotifyRoleMessage(ctx context.Context, roleID int64, message any, anywhere bool) bool {
	pid, _ := service.GetRoleGrain(roleID, anywhere)
	if pid == nil {
		glog.Debugf(ctx, "notify failed, role grain not found, roleID:%d message:%v", roleID, message)
		return false
	}

	if err := gxyactor.ActorSystem().Notify(pid, message); err != nil {
		glog.Errorf(ctx, "notify failed, roleID:%d message:%v err:%+v", roleID, message, err)
		return false
	}
	return true
}
