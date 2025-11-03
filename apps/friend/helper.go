package friend

import (
	"context"
	"gserver/apps/api"
	"gserver/core/gxyhttp"

	"github.com/gogf/gf/v2/util/gconv"
)

// 返回成功响应
// handleApply 处理好友申请
func FriendApply(ctx context.Context, roleID int64, friendID int64, source string) (*api.ApplyFriendRes, error) {
	req := api.ApplyFriendReq{
		RoleID:   roleID,
		FriendID: friendID,
		Source:   source,
	}
	uri := gxyhttp.GetReqUri(req)
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, SERVICE_NAME, uri, req)
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
func DealApply(ctx context.Context, roleID int64, applyerID int64, deal int32) (*api.DealApplyRes, error) {
	req := api.DealApplyReq{
		RoleID:    roleID,
		ApplyerID: applyerID,
		Deal:      deal,
	}
	uri := gxyhttp.GetReqUri(req)
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, SERVICE_NAME, uri, req)
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
func FriendDelete(ctx context.Context, roleID int64, friendID int64) (*api.DeleteFriendRes, error) {
	req := api.DeleteFriendReq{
		RoleID:   roleID,
		FriendID: friendID,
	}

	uri := gxyhttp.GetReqUri(req)
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, SERVICE_NAME, uri, req)
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
func GetFriendInfo(ctx context.Context, roleID int64) (*api.GetFriendListRes, error) {
	req := api.GetFriendListReq{
		RoleID: roleID,
	}

	uri := gxyhttp.GetReqUri(req)
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, SERVICE_NAME, uri, req)
	if err != nil {
		return nil, err
	}

	res := &api.GetFriendListRes{}
	if err := gconv.Struct(rsp.Data, res); err != nil {
		return nil, err
	}
	return res, nil
}
