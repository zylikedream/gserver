package logic

import (
	"context"
	"fmt"
	"gserver/protocol/pb"
	"time"
)

type RoleBasic struct {
	RoleModule
	RoleID   int64     `bson:"role_id"`
	RoleName string    `bson:"role_name"`
	LoginTm  time.Time `bson:"login_tm"`
	CreateTm time.Time `bson:"create_tm"` // 创角时间
	VipLv    int       `bson:"vip_lv"`
}

func NewRoleBasic() *RoleBasic {
	return &RoleBasic{}
}

func (r *RoleBasic) ReqBasicSetName(ctx context.Context, req *pb.ReqBasicSetName) (*pb.RspBasicSetName, error) {
	if !r.isNameValid(req.Name) {
		return nil, fmt.Errorf("name unvalid:%s", req.Name)
	}
	rsp := &pb.RspBasicSetName{
		Name: req.Name,
	}
	r.RoleName = req.Name
	return rsp, nil
}

// --------------------proto handlers end-------------

func (r *RoleBasic) isNameValid(string) bool {
	return true
}
