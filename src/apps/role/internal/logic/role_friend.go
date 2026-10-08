package logic

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"gserver/core/gxyhttp"
	"gserver/core/gxylog"
	"gserver/protocol/pb"
	"gserver/src/apps/api"
	"gserver/src/lib/rolelib"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/util/gconv"

	"github.com/cockroachdb/errors"
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
	// 数组参数用 gf 的 bs[]=1&bs[]=2 约定:逗号形式绑不到 []int64,
	// 重复键(bs=1&bs=2)只保留最后一个,两者都会静默丢参数。
	query := url.Values{}
	query.Set("a", strconv.FormatInt(a, 10))
	for _, id := range ids {
		query.Add("bs[]", strconv.FormatInt(id, 10))
	}
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "friend", path+"?"+query.Encode())
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

// callFriendIsFriend 经 friend 服务回答「两人是否已是好友」。
// 此前本文件直接查 friend_relation 表——那是 friend 应用持有的表,role 直查属于
// 跨应用越界(表结构变更时编译照过、运行时才炸,且与 friend 侧的判定逻辑会漂移)。
//
// 错误语义:一律返回 false,即 fail-closed。调用方据此拒绝准入(steal / 私聊)。
// 这是刻意选择——friend 服务不可用时宁可拒绝一次操作,也不要放行一个未经
// 校验的准入。代价是 friend 服务抖动会让 steal 与私聊同时不可用。
func callFriendIsFriend(ctx context.Context, a, b int64) bool {
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "friend",
		fmt.Sprintf("is_friend?player_id=%d&target_id=%d", a, b))
	if err != nil {
		gxylog.Warn(ctx, "call friend is_friend failed",
			gxylog.Num("player_id", a), gxylog.Num("target_id", b), gxylog.Err(err))
		return false
	}
	var out struct {
		IsFriend bool `json:"is_friend"`
	}
	if err := gconv.Scan(rsp.Data, &out); err != nil {
		gxylog.Warn(ctx, "parse is_friend response failed",
			gxylog.Num("player_id", a), gxylog.Num("target_id", b), gxylog.Err(err))
		return false
	}
	return out.IsFriend
}
