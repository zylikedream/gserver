package logic

import (
	"context"
	"gserver/core/gxyhttp"
	"gserver/core/gxypgx"
	"gserver/src/apps/api"

	"github.com/gogf/gf/v2/frame/g"
)

type FriendHandler struct {
	g.Meta `method:"POST"`
}

type SendRequestReq struct {
	g.Meta `path:"/send_request"`
	A      int64   `p:"a"`
	Bs     []int64 `p:"bs"`
}

func (h *FriendHandler) SendRequest(ctx context.Context, req *SendRequestReq) (any, error) {
	cfg := LoadConfig()
	return batchResult(req.Bs, func(id int64) error {
		return SendRequest(ctx, req.A, id, cfg)
	})
}

type AcceptRequestReq struct {
	g.Meta `path:"/accept_request"`
	A      int64   `p:"a"`
	Bs     []int64 `p:"bs"`
}

func (h *FriendHandler) AcceptRequest(ctx context.Context, req *AcceptRequestReq) (any, error) {
	cfg := LoadConfig()
	return batchResult(req.Bs, func(id int64) error {
		return AcceptRequest(ctx, req.A, id, cfg)
	})
}

type RejectRequestReq struct {
	g.Meta `path:"/reject_request"`
	A      int64   `p:"a"`
	Bs     []int64 `p:"bs"`
}

func (h *FriendHandler) RejectRequest(ctx context.Context, req *RejectRequestReq) (any, error) {
	return batchResult(req.Bs, func(id int64) error {
		return RejectRequest(ctx, req.A, id)
	})
}

type RemoveFriendReq struct {
	g.Meta `path:"/remove_friend"`
	A      int64 `p:"a"`
	B      int64 `p:"b"`
}

func (h *FriendHandler) RemoveFriend(ctx context.Context, req *RemoveFriendReq) (any, error) {
	cfg := LoadConfig()
	err := RemoveFriend(ctx, req.A, req.B, cfg)
	return nil, mapErr(err)
}

type ListReq struct {
	g.Meta   `path:"/list"`
	PlayerID int64 `p:"player_id"`
}

func (h *FriendHandler) List(ctx context.Context, req *ListReq) (any, error) {
	var data []FriendData
	gxypgx.DB().WithContext(ctx).Find(&data, req.PlayerID)
	if len(data) == 0 {
		return &FriendData{PlayerID: req.PlayerID}, nil
	}
	return &data[0], nil
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	return gxyhttp.NewErrCode(1, err.Error())
}

func batchResult(ids []int64, fn func(int64) error) (any, error) {
	items := make([]api.FriendBatchItem, 0, len(ids))
	for _, id := range ids {
		err := fn(id)
		item := api.FriendBatchItem{TargetID: id, Success: err == nil}
		if err != nil {
			item.Error = err.Error()
		}
		items = append(items, item)
	}
	return items, nil
}
