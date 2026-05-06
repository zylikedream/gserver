package logic

import (
	"context"
	"errors"
	"gserver/core/gxyhttp"
	"gserver/core/gxypgx"

	"github.com/gogf/gf/v2/frame/g"
	"gorm.io/gorm"
)

type FriendHandler struct {
	g.Meta `method:"POST"`
}

type SendRequestReq struct {
	g.Meta `path:"/send_request"`
	A int64 `p:"a"`
	B int64 `p:"b"`
}

func (h *FriendHandler) SendRequest(ctx context.Context, req *SendRequestReq) (any, error) {
	cfg := LoadConfig()
	err := SendRequest(ctx, req.A, req.B, cfg)
	return nil, mapErr(err)
}

type AcceptRequestReq struct {
	g.Meta `path:"/accept_request"`
	A int64 `p:"a"`
	B int64 `p:"b"`
}

func (h *FriendHandler) AcceptRequest(ctx context.Context, req *AcceptRequestReq) (any, error) {
	cfg := LoadConfig()
	err := AcceptRequest(ctx, req.A, req.B, cfg)
	return nil, mapErr(err)
}

type RejectRequestReq struct {
	g.Meta `path:"/reject_request"`
	A int64 `p:"a"`
	B int64 `p:"b"`
}

func (h *FriendHandler) RejectRequest(ctx context.Context, req *RejectRequestReq) (any, error) {
	err := RejectRequest(ctx, req.A, req.B)
	return nil, mapErr(err)
}

type RemoveFriendReq struct {
	g.Meta `path:"/remove_friend"`
	A int64 `p:"a"`
	B int64 `p:"b"`
}

func (h *FriendHandler) RemoveFriend(ctx context.Context, req *RemoveFriendReq) (any, error) {
	cfg := LoadConfig()
	err := RemoveFriend(ctx, req.A, req.B, cfg)
	return nil, mapErr(err)
}

type ListReq struct {
	g.Meta  `path:"/list"`
	PlayerID int64 `p:"player_id"`
}

func (h *FriendHandler) List(ctx context.Context, req *ListReq) (any, error) {
	var data FriendData
	err := gxypgx.DB().WithContext(ctx).First(&data, req.PlayerID).Error
	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		return &FriendData{PlayerID: req.PlayerID}, nil
	}
	return &data, mapErr(err)
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	return gxyhttp.NewErrCode(1, err.Error())
}
