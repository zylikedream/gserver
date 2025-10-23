package logic

import (
	"context"
	"gserver/protocol/pb"
	friend "gserver/service/friend/api"
)

type RoleFriend struct {
	FriendList    []friend.FriendInfo
	ApplySendList []friend.FriendInfo
	ApplyRecv     []friend.FriendInfo
}

func (r *RoleFriend) ReqFriendInfo(ctx context.Context, req *pb.ReqFriendInfo) (*pb.RspFriendInfo, error) {
	return &pb.RspFriendInfo{}, nil
}
