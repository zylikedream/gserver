package friend

import (
	"context"

	"gserver/core/gxyhttp"
	"gserver/service"
	"gserver/service/friend/api"
	"gserver/service/friend/internal/logic"

	"github.com/gogf/gf/v2/util/gconv"
)

type friendService struct {
	gxyhttp.HttpService
	controller *logic.FriendController
}

var friendSvc = newFriendService()

func FriendService() *friendService {
	return friendSvc
}

func newFriendService() *friendService {
	return &friendService{
		controller: logic.NewFriendController(),
	}
}

func (f *friendService) Name() string {
	return service.FRIEND_SERVICE

}

func (f *friendService) OnModInit(ctx context.Context) error {
	f.SetHandler(ctx, f.Name(), logic.NewFriendController())
	return nil
}

// 返回成功响应
// handleApply 处理好友申请
func (f *friendService) FriendApply(ctx context.Context, roleID int64, friendID int64, source string) (*api.ApplyFriendRes, error) {
	req := api.ApplyFriendReq{
		RoleID:   roleID,
		FriendID: friendID,
		Source:   source,
	}
	uri := f.GetReqUri(req)
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, f.Name(), uri, req)
	if err != nil {
		return nil, err
	}

	res := &api.ApplyFriendRes{}
	if err := gconv.Struct(rsp.Data, res); err != nil {
		return nil, err
	}
	return res, nil
}

// handleDealApply 处理好友申请回应
func (f *friendService) DealApply(ctx context.Context, roleID int64, applyerID int64, deal int32) (*api.DealApplyRes, error) {
	req := api.DealApplyReq{
		RoleID:    roleID,
		ApplyerID: applyerID,
		Deal:      deal,
	}
	uri := f.GetReqUri(req)
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, f.Name(), uri, req)
	if err != nil {
		return nil, err
	}

	res := &api.DealApplyRes{}
	if err := gconv.Struct(rsp.Data, res); err != nil {
		return nil, err
	}
	return res, nil
}

// handleDeleteFriend 处理删除好友
func (f *friendService) FriendDelete(ctx context.Context, roleID int64, friendID int64) (*api.DeleteFriendRes, error) {
	req := api.DeleteFriendReq{
		RoleID:   roleID,
		FriendID: friendID,
	}

	uri := f.GetReqUri(req)
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, f.Name(), uri, req)
	if err != nil {
		return nil, err
	}

	res := &api.DeleteFriendRes{}
	if err := gconv.Struct(rsp.Data, res); err != nil {
		return nil, err
	}
	return res, nil
}

// handleGetFriendList 处理获取好友列表
func (f *friendService) GetFriendInfo(ctx context.Context, roleID int64) (*api.GetFriendListRes, error) {
	req := api.GetFriendListReq{
		RoleID: roleID,
	}
	uri := f.GetReqUri(req)
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, f.Name(), uri, req)
	if err != nil {
		return nil, err
	}

	res := &api.GetFriendListRes{}
	if err := gconv.Struct(rsp.Data, res); err != nil {
		return nil, err
	}
	return res, nil
}
