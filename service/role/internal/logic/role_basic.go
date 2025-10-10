package logic

import (
	"context"
	"fmt"
	"gserver/protocol/pb"
	"time"
)

type RoleBasic struct {
	RoleModule `bson:"inline"`
	RoleName   string    `bson:"role_name"`
	Head       string    `bson:"head"`
	LoginTm    time.Time `bson:"login_tm"`
	LogoutTm   time.Time `bson:"login_tm"`
	CreateTm   time.Time `bson:"create_tm"` // 创角时间
	VipLv      int       `bson:"vip_lv"`
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

func (r *RoleBasic) ReqBasicInfo(ctx context.Context, req *pb.ReqBasicInfo) (*pb.RspBasicInfo, error) {
	return &pb.RspBasicInfo{
		RoleId:   r.RoleID,
		Name:     r.RoleName,
		CreateTm: r.CreateTm.Unix(),
		Head:     r.Head,
	}, nil
}

func (r *RoleBasic) ReqBasicSetHead(ctx context.Context, req *pb.ReqBasicSetHead) (*pb.RspBasicSetHead, error) {
	rsp := &pb.RspBasicSetHead{
		Head: req.Head,
	}
	r.Head = req.Head
	return rsp, nil
}

// --------------------proto handlers end-------------

func (r *RoleBasic) isNameValid(string) bool {
	return true
}
