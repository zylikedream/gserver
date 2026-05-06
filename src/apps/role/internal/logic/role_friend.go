package logic

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"gserver/core/gxypgx"
	"gserver/gameconfig"
	"gserver/protocol/pb"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/gclient"
	"github.com/gogf/gf/v2/os/glog"
)

const friendServiceURL = "http://127.0.0.1:6801"

// ---- write operations (call friend service via HTTP) ----

func (r *RoleMain) ReqSendRequest(ctx context.Context, req *pb.ReqSendRequest) (*pb.RspSendRequest, error) {
	err := callFriendWrite(ctx, "/friend/send_request", r.RoleID, req.TargetId)
	return &pb.RspSendRequest{}, err
}

func (r *RoleMain) ReqAcceptRequest(ctx context.Context, req *pb.ReqAcceptRequest) (*pb.RspAcceptRequest, error) {
	err := callFriendWrite(ctx, "/friend/accept_request", r.RoleID, req.FromId)
	return &pb.RspAcceptRequest{}, err
}

func (r *RoleMain) ReqRejectRequest(ctx context.Context, req *pb.ReqRejectRequest) (*pb.RspRejectRequest, error) {
	err := callFriendWrite(ctx, "/friend/reject_request", r.RoleID, req.FromId)
	return &pb.RspRejectRequest{}, err
}

func (r *RoleMain) ReqRemoveFriend(ctx context.Context, req *pb.ReqRemoveFriend) (*pb.RspRemoveFriend, error) {
	err := callFriendWrite(ctx, "/friend/remove_friend", r.RoleID, req.TargetId)
	return &pb.RspRemoveFriend{}, err
}

// ---- read operations ----

func (r *RoleMain) ReqFriendList(ctx context.Context, req *pb.ReqFriendList) (*pb.RspFriendList, error) {
	cfg := gameconfig.GameConfig().TbFriendConfig.Get()

	friendIDs, err := callFriendList(ctx, r.RoleID)
	if err != nil {
		return nil, err
	}

	rsp := &pb.RspFriendList{
		Limit: cfg.FriendMaxCount,
		Total: int32(len(friendIDs)),
	}
	for _, friendID := range friendIDs {
		public := GetRolePublic(ctx, friendID)
		if public == nil {
			continue
		}
		rsp.Friends = append(rsp.Friends, &pb.PFriendInfo{
			PlayerInfo: public,
		})
	}
	return rsp, nil
}

func (r *RoleMain) ReqApplyList(ctx context.Context, req *pb.ReqApplyList) (*pb.RspApplyList, error) {
	data, err := callFriendData(ctx, r.RoleID)
	if err != nil {
		return &pb.RspApplyList{}, nil
	}

	rsp := &pb.RspApplyList{}
	for _, fromID := range data.Incoming {
		public := GetRolePublic(ctx, fromID)
		if public == nil {
			continue
		}
		rsp.Incoming = append(rsp.Incoming, &pb.PApplyInfo{
			PlayerInfo: public,
			Status:     0,
		})
	}
	for _, toID := range data.Outgoing {
		public := GetRolePublic(ctx, toID)
		if public == nil {
			continue
		}
		rsp.Outgoing = append(rsp.Outgoing, &pb.PApplyInfo{
			PlayerInfo: public,
			Status:     0,
		})
	}
	return rsp, nil
}

func (r *RoleMain) ReqSearchPlayer(ctx context.Context, req *pb.ReqSearchPlayer) (*pb.RspSearchPlayer, error) {
	cfg := gameconfig.GameConfig().TbFriendConfig.Get()

	var publics []struct {
		RoleID      int64  `gorm:"column:role_id"`
		Name        string `gorm:"column:name"`
		Head        string `gorm:"column:head"`
		CreateTime  int64  `gorm:"column:create_time"`
		Level       int32  `gorm:"column:level"`
		LastLoginAt int64  `gorm:"column:last_login_at"`
		IsOnline    bool   `gorm:"column:is_online"`
	}
	err := gxypgx.DB().WithContext(ctx).
		Table("role_public").
		Where("name LIKE ?", "%"+req.Name+"%").
		Limit(int(cfg.SearchResultLimit)).
		Find(&publics).Error
	if err != nil {
		return nil, err
	}

	rsp := &pb.RspSearchPlayer{}
	for _, p := range publics {
		info := &pb.PPlayerInfo{
			PlayerInfo: &pb.PRolePublic{
				RoleId:      p.RoleID,
				Name:        p.Name,
				Head:        p.Head,
				CreateTime:  p.CreateTime,
				Level:       p.Level,
				LastLoginAt: p.LastLoginAt,
				IsOnline:    p.IsOnline,
			},
		}
		if p.RoleID == r.RoleID {
			info.Relation = 0
		} else {
			relation, err := getRelation(ctx, r.RoleID, p.RoleID)
			if err != nil {
				glog.Warningf(ctx, "getRelation error: %v", err)
			}
			info.Relation = int32(relation)
		}
		rsp.Players = append(rsp.Players, info)
	}
	return rsp, nil
}

// ---- HTTP helpers ----

type friendResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func callFriendGet(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	vals := url.Values{}
	for k, v := range params {
		vals.Set(k, v)
	}
	url := friendServiceURL + path + "?" + vals.Encode()
	rsp, err := gclient.New().Get(ctx, url)
	if err != nil {
		return nil, gerror.Wrapf(err, "friend service call failed: %s", path)
	}
	defer rsp.Body.Close()
	body := rsp.ReadAll()

	var result friendResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, gerror.Wrapf(err, "unmarshal friend response failed")
	}
	if result.Code != 0 {
		return nil, gerror.New(result.Message)
	}
	return result.Data, nil
}

func callFriendWrite(ctx context.Context, path string, a, b int64) error {
	_, err := callFriendGet(ctx, path, map[string]string{
		"a": strconv.FormatInt(a, 10),
		"b": strconv.FormatInt(b, 10),
	})
	return err
}

type friendDataJSON struct {
	PlayerID  int64   `json:"player_id"`
	Friends   []int64 `json:"friends"`
	Incoming  []int64 `json:"incoming"`
	Outgoing  []int64 `json:"outgoing"`
}

func callFriendData(ctx context.Context, playerID int64) (*friendDataJSON, error) {
	data, err := callFriendGet(ctx, "/friend/list", map[string]string{
		"player_id": strconv.FormatInt(playerID, 10),
	})
	if err != nil {
		return nil, err
	}
	var fd friendDataJSON
	if err := json.Unmarshal(data, &fd); err != nil {
		return nil, gerror.Wrap(err, "parse friend data failed")
	}
	return &fd, nil
}

func callFriendList(ctx context.Context, playerID int64) ([]int64, error) {
	fd, err := callFriendData(ctx, playerID)
	if err != nil {
		return nil, err
	}
	return fd.Friends, nil
}

// ---- relation check ----

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
	for _, id := range fd.Friends {
		if id == targetID {
			return relationFriend, nil
		}
	}
	for _, id := range fd.Outgoing {
		if id == targetID {
			return relationApplied, nil
		}
	}
	return relationStranger, nil
}
