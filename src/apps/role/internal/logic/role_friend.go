package logic

import (
	"context"
	"fmt"
	"strings"

	"gserver/core/gxyhttp"
	"gserver/core/gxylog"
	"gserver/protocol/pb"
	"gserver/src/apps/api"
	"gserver/src/lib/rolelib"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/util/gconv"

	"github.com/cockroachdb/errors"
	"gorm.io/gorm"
)

type RoleFriend struct {
	RoleModule
}

// ---- write operations (call friend service via HTTP) ----

func (r *RoleFriend) ReqFriendSendRequest(ctx context.Context, req *pb.ReqFriendSendRequest) (*pb.RspFriendSendRequest, error) {
	successIDs, err := callFriendBatch(ctx, "send_request", r.RoleID, req.TargetIds)
	rsp := &pb.RspFriendSendRequest{}
	if len(successIDs) == 0 {
		return rsp, err
	}
	for _, id := range successIDs {
		public := GetRolePublic(ctx, r.Deps(), id)
		if public == nil {
			continue
		}
		rsp.Friends = append(rsp.Friends, &pb.PFriendInfo{PlayerInfo: public})
		_ = rolelib.PublishRoleNotify(ctx, id, &pb.NotifyFriendNewRequest{
			ApplyInfo: &pb.PApplyInfo{
				PlayerInfo: GetRolePublic(ctx, r.Deps(), r.RoleID),
			},
		})
	}
	return rsp, nil
}

func (r *RoleFriend) ReqFriendAcceptRequest(ctx context.Context, req *pb.ReqFriendAcceptRequest) (*pb.RspFriendAcceptRequest, error) {
	successIDs, err := callFriendBatch(ctx, "accept_request", r.RoleID, req.FromIds)
	rsp := &pb.RspFriendAcceptRequest{}
	if len(successIDs) == 0 {
		return rsp, err
	}
	for _, id := range successIDs {
		public := GetRolePublic(ctx, r.Deps(), id)
		if public == nil {
			continue
		}
		rsp.Friends = append(rsp.Friends, &pb.PFriendInfo{PlayerInfo: public})
		_ = rolelib.PublishRoleNotify(ctx, id, &pb.NotifyNewFriend{
			FriendInfo: &pb.PFriendInfo{PlayerInfo: GetRolePublic(ctx, r.Deps(), r.RoleID)},
		})
	}
	return rsp, nil
}

func (r *RoleFriend) ReqFriendRejectRequest(ctx context.Context, req *pb.ReqFriendRejectRequest) (*pb.RspFriendRejectRequest, error) {
	_, err := callFriendBatch(ctx, "reject_request", r.RoleID, req.FromIds)
	return &pb.RspFriendRejectRequest{}, err
}

func (r *RoleFriend) ReqFriendRemove(ctx context.Context, req *pb.ReqFriendRemove) (*pb.RspFriendRemove, error) {
	err := callFriendWrite(ctx, "remove_friend", r.RoleID, req.TargetId)
	return &pb.RspFriendRemove{}, err
}

// ---- read operations ----

func (r *RoleFriend) ReqFriendList(ctx context.Context, req *pb.ReqFriendList) (*pb.RspFriendList, error) {
	cfg := r.Cfg().TbFriendConfig.Get()

	friendIDs, err := callFriendList(ctx, r.RoleID)
	if err != nil {
		return nil, err
	}

	rsp := &pb.RspFriendList{
		Limit: cfg.FriendMaxCount,
		Total: int32(len(friendIDs)),
	}
	for _, f := range friendIDs {
		public := GetRolePublic(ctx, r.Deps(), f.PlayerID)
		if public == nil {
			continue
		}
		rsp.Friends = append(rsp.Friends, &pb.PFriendInfo{
			PlayerInfo:  public,
			FriendSince: f.AddedAt,
		})
	}
	return rsp, nil
}

func (r *RoleFriend) ReqFriendApplyList(ctx context.Context, req *pb.ReqFriendApplyList) (*pb.RspFriendApplyList, error) {
	data, err := callFriendData(ctx, r.RoleID)
	if err != nil {
		return &pb.RspFriendApplyList{}, nil
	}

	rsp := &pb.RspFriendApplyList{}
	for _, a := range data.Incoming {
		public := GetRolePublic(ctx, r.Deps(), a.PlayerID)
		if public == nil {
			continue
		}
		rsp.Incoming = append(rsp.Incoming, &pb.PApplyInfo{
			PlayerInfo: public,
			ApplyAt:    a.ApplyAt,
			Status:     0,
		})
	}
	for _, a := range data.Outgoing {
		public := GetRolePublic(ctx, r.Deps(), a.PlayerID)
		if public == nil {
			continue
		}
		rsp.Outgoing = append(rsp.Outgoing, &pb.PApplyInfo{
			PlayerInfo: public,
			ApplyAt:    a.ApplyAt,
			Status:     0,
		})
	}
	return rsp, nil
}

func (r *RoleFriend) ReqFriendSearchPlayer(ctx context.Context, req *pb.ReqFriendSearchPlayer) (*pb.RspFriendSearchPlayer, error) {
	cfg := r.Cfg().TbFriendConfig.Get()

	publics := []RolePublicState{}
	err := r.DB().WithContext(ctx).
		Table("role_public").
		Where("name LIKE ?", "%"+req.Name+"%").
		Limit(int(cfg.SearchResultLimit)).
		Find(&publics).Error
	if err != nil {
		return nil, err
	}

	rsp := &pb.RspFriendSearchPlayer{}
	for _, p := range publics {
		info := &pb.PPlayerInfo{
			PlayerInfo: PRolePublic(&p),
		}
		if p.RoleID == r.RoleID {
			info.Relation = 0
		} else {
			relation, err := getRelation(ctx, r.RoleID, p.RoleID)
			if err != nil {
				gxylog.Warn(ctx, "getRelation error", gxylog.Err(err))
			}
			info.Relation = int32(relation)
		}
		rsp.Players = append(rsp.Players, info)
	}
	return rsp, nil
}

// ---- HTTP helpers ----

type friendEntryJSON struct {
	PlayerID int64 `json:"player_id"`
	AddedAt  int64 `json:"added_at"`
}

type applyEntryJSON struct {
	PlayerID int64 `json:"player_id"`
	ApplyAt  int64 `json:"apply_at"`
}

type friendDataJSON struct {
	PlayerID int64             `json:"player_id"`
	Friends  []friendEntryJSON `json:"friends"`
	Incoming []applyEntryJSON  `json:"incoming"`
	Outgoing []applyEntryJSON  `json:"outgoing"`
}

func callFriendWrite(ctx context.Context, path string, a, b int64) error {
	_, err := gxyhttp.HttpSystem().PostService(ctx, "friend",
		fmt.Sprintf("%s?a=%d&b=%d", path, a, b))
	return err
}

func callFriendBatch(ctx context.Context, path string, a int64, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = fmt.Sprintf("%d", id)
	}
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "friend",
		fmt.Sprintf("%s?a=%d&bs=%s", path, a, strings.Join(strs, ",")))
	if err != nil {
		return nil, err
	}

	result := []api.FriendBatchItem{}
	if err := gconv.Scan(rsp.Data, &result); err != nil {
		return nil, gerror.Wrap(err, "parse batch result failed")
	}
	var successIDs []int64
	for _, item := range result {
		if item.Error != "" {
			err = errors.New(item.Error)
		}
		if item.Success {
			successIDs = append(successIDs, item.TargetID)
		}
	}
	return successIDs, err
}

func callFriendData(ctx context.Context, playerID int64) (*friendDataJSON, error) {
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "friend",
		fmt.Sprintf("list?player_id=%d", playerID))
	if err != nil {
		return nil, err
	}
	var fd friendDataJSON
	if err := gconv.Scan(rsp.Data, &fd); err != nil {
		return nil, gerror.Wrap(err, "parse friend data failed")
	}
	return &fd, nil
}

func callFriendList(ctx context.Context, playerID int64) ([]friendEntryJSON, error) {
	fd, err := callFriendData(ctx, playerID)
	if err != nil {
		return nil, err
	}
	return fd.Friends, nil
}

type relation int32

const (
	relationStranger relation = 1
	relationApplied  relation = 2
	relationFriend   relation = 3
)

func getRelation(ctx context.Context, myID, targetID int64) (relation, error) {
	fd, err := callFriendData(ctx, myID)
	if err != nil {
		return relationStranger, nil
	}
	for _, f := range fd.Friends {
		if f.PlayerID == targetID {
			return relationFriend, nil
		}
	}
	for _, a := range fd.Outgoing {
		if a.PlayerID == targetID {
			return relationApplied, nil
		}
	}
	return relationStranger, nil
}

// isFriend 可替换函数变量:测试可注入 mock 实现(编译期安全)。
var isFriend = func(ctx context.Context, db *gorm.DB, myID, targetID int64) bool {
	var count int64
	err := db.WithContext(ctx).Table("friend_relation").
		Where("player_id = ? AND friend_id = ?", myID, targetID).
		Count(&count).Error
	if err != nil {
		return false
	}
	return count > 0
}
