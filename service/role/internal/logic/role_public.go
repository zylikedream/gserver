package logic

import (
	"context"
	"gserver/protocol/pb"
)

type RolePublic struct {
	RoleModule
}

func (r *RolePublic) GetRolePublic() *pb.PRolePublic {
	role := r.Role
	return &pb.PRolePublic{
		RoleId:     role.RoleID,
		Name:       role.Basic.RoleName,
		Head:       role.Basic.Head,
		CreateTime: role.Basic.CreateTm.Unix(),
	}
}

func (r *RolePublic) OnModStop(ctx context.Context) error {
	if err := r.Save(ctx); err != nil {
		return err
	}
	return nil
}

func (r *RolePublic) Save(ctx context.Context) error {
	return nil
}
