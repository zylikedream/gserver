package logic

import (
	"context"

	"gserver/core/gxypgx"
	"gserver/gameconfig"
	"gserver/protocol/pb"
	friendpkg "gserver/service/friend"

	"github.com/gogf/gf/v2/os/glog"
)

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
			info.Relation = 0 // 自己
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

func (r *RoleMain) ReqSendRequest(ctx context.Context, req *pb.ReqSendRequest) (*pb.RspSendRequest, error) {
	cfg := friendpkg.LoadConfig()
	err := friendpkg.SendRequest(ctx, r.RoleID, req.TargetId, cfg)
	return &pb.RspSendRequest{}, err
}

func (r *RoleMain) ReqAcceptRequest(ctx context.Context, req *pb.ReqAcceptRequest) (*pb.RspAcceptRequest, error) {
	cfg := friendpkg.LoadConfig()
	err := friendpkg.AcceptRequest(ctx, r.RoleID, req.FromId, cfg)
	return &pb.RspAcceptRequest{}, err
}

func (r *RoleMain) ReqRejectRequest(ctx context.Context, req *pb.ReqRejectRequest) (*pb.RspRejectRequest, error) {
	err := friendpkg.RejectRequest(ctx, r.RoleID, req.FromId)
	return &pb.RspRejectRequest{}, err
}

func (r *RoleMain) ReqFriendList(ctx context.Context, req *pb.ReqFriendList) (*pb.RspFriendList, error) {
	cfg := gameconfig.GameConfig().TbFriendConfig.Get()

	var data friendpkg.FriendData
	err := gxypgx.DB().WithContext(ctx).First(&data, r.RoleID).Error
	if err != nil {
		return &pb.RspFriendList{Limit: cfg.FriendMaxCount}, nil
	}

	rsp := &pb.RspFriendList{
		Limit: cfg.FriendMaxCount,
		Total: int32(len(data.Friends)),
	}
	for _, friendID := range data.Friends {
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
	var data friendpkg.FriendData
	err := gxypgx.DB().WithContext(ctx).First(&data, r.RoleID).Error
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

func (r *RoleMain) ReqRemoveFriend(ctx context.Context, req *pb.ReqRemoveFriend) (*pb.RspRemoveFriend, error) {
	cfg := friendpkg.LoadConfig()
	err := friendpkg.RemoveFriend(ctx, r.RoleID, req.TargetId, cfg)
	return &pb.RspRemoveFriend{}, err
}

// ---- helpers ----

type relation int32

const (
	relationStranger relation = 1
	relationApplied  relation = 2
	relationFriend   relation = 3
)

func getRelation(ctx context.Context, myID, targetID int64) (relation, error) {
	var data friendpkg.FriendData
	err := gxypgx.DB().WithContext(ctx).First(&data, myID).Error
	if err != nil {
		return relationStranger, nil
	}
	if data.Friends.Has(targetID) {
		return relationFriend, nil
	}
	if data.Outgoing.Has(targetID) {
		return relationApplied, nil
	}
	return relationStranger, nil
}
