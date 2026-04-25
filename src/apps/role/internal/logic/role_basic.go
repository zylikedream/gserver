package logic

import (
	"context"
	"fmt"
	"gserver/protocol/pb"
	"time"
)

type RoleBasicState struct {
	RolePersistState `db:"inline"`
	RoleName         string    `db:"role_name"`
	Head             string    `db:"head"`
	LoginTm          time.Time `db:"login_tm"`
	LogoutTm         time.Time `db:"logout_tm"`
	CreateTm         time.Time `db:"create_tm"` // 创角时间
	VipLv            int       `db:"vip_lv"`
}

func (r *RoleBasicState) GetIndexes() []string {
	return []string{"update_at"}
}

type RoleBasic struct {
	RoleModule
	RoleBasicState
}

var _ IRoleModule = (*RoleBasic)(nil)

func (r *RoleBasic) PersistState() IPersistState {
	return &r.RoleBasicState
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
