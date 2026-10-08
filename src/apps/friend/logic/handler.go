package logic

import (
	"context"
	"gserver/core/gxyhttp"
	"gserver/core/gxypgx"

	"gorm.io/gorm"
	"gserver/src/apps/api"
	"gserver/src/pkg/gameconfig"

	"github.com/gogf/gf/v2/frame/g"
)

type FriendHandler struct {
	g.Meta `method:"POST"`
	db     *gorm.DB
	cfg    *gameconfig.GameConfig
}

// NewFriendHandler 构造注入依赖(组装根)。
func NewFriendHandler() *FriendHandler {
	return &FriendHandler{db: gxypgx.DB(), cfg: gameconfig.Get()}
}

type SendRequestReq struct {
	g.Meta `path:"/send_request"`
	A      int64   `p:"a"`
	Bs     []int64 `p:"bs"`
}

func (h *FriendHandler) SendRequest(ctx context.Context, req *SendRequestReq) (any, error) {
	cfg := LoadConfig(h.cfg)
	return batchResult(req.Bs, func(id int64) error {
		return SendRequest(ctx, req.A, id, cfg, h.db)
	})
}

type AcceptRequestReq struct {
	g.Meta `path:"/accept_request"`
	A      int64   `p:"a"`
	Bs     []int64 `p:"bs"`
}

func (h *FriendHandler) AcceptRequest(ctx context.Context, req *AcceptRequestReq) (any, error) {
	cfg := LoadConfig(h.cfg)
	return batchResult(req.Bs, func(id int64) error {
		return AcceptRequest(ctx, req.A, id, cfg, h.db)
	})
}

type RejectRequestReq struct {
	g.Meta `path:"/reject_request"`
	A      int64   `p:"a"`
	Bs     []int64 `p:"bs"`
}

func (h *FriendHandler) RejectRequest(ctx context.Context, req *RejectRequestReq) (any, error) {
	return batchResult(req.Bs, func(id int64) error {
		return RejectRequest(ctx, req.A, id, h.db)
	})
}

type RemoveFriendReq struct {
	g.Meta `path:"/remove_friend"`
	A      int64 `p:"a"`
	B      int64 `p:"b"`
}

func (h *FriendHandler) RemoveFriend(ctx context.Context, req *RemoveFriendReq) (any, error) {
	cfg := LoadConfig(h.cfg)
	err := RemoveFriend(ctx, req.A, req.B, cfg, h.db)
	return nil, mapErr(err)
}

type ListReq struct {
	g.Meta   `path:"/list"`
	PlayerID int64 `p:"player_id"`
}

func (h *FriendHandler) List(ctx context.Context, req *ListReq) (any, error) {
	var data []FriendData
	h.db.WithContext(ctx).Find(&data, req.PlayerID)
	if len(data) == 0 {
		return &FriendData{PlayerID: req.PlayerID}, nil
	}
	return &data[0], nil
}

type IsFriendReq struct {
	g.Meta `path:"/is_friend"`
	// PlayerID 查询方;TargetID 目标方。friend_relation 一条好友存两行,
	// 所以两个方向必须一致——本接口只查 (player_id, friend_id) 单行,
	// 对称性由 addRelation/removeRelation 维护两行来保证。
	PlayerID int64 `p:"player_id"`
	TargetID int64 `p:"target_id"`
}

// IsFriend 回答「两人是否已互为好友」。
// 这条接口存在的理由:调用方(role)此前直查 friend_relation 表,属于跨应用读
// 对方的表——friend 改表结构时 role 侧编译照过、运行时才炸,且两份好友判定会漂移。
// 改为经本接口后,表的归属只有 friend 一处。
func (h *FriendHandler) IsFriend(ctx context.Context, req *IsFriendReq) (any, error) {
	var count int64
	err := h.db.WithContext(ctx).Model(&FriendRelation{}).
		Where("player_id = ? AND friend_id = ?", req.PlayerID, req.TargetID).
		Count(&count).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return map[string]bool{"is_friend": count > 0}, nil
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
