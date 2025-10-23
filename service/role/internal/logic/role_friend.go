package logic

import (
	"context"
	"gserver/protocol/pb"
	friend "gserver/service/friend/api"
)

type friendCache struct {
	friendList    []friend.FriendInfo
	applySendList []friend.FriendInfo
	applyRecv     []friend.FriendInfo
}

type RoleFriend struct {
	RoleModule `bson:"inline"`
	cache      friendCache
}

func (r *RoleFriend) ReqFriendInfo(ctx context.Context, req *pb.ReqFriendInfo) (*pb.RspFriendInfo, error) {
	return &pb.RspFriendInfo{}, nil
}
