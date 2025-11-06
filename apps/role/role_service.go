package role

import (
	"context"
	"gserver/apps/role/internal/logic"
	"gserver/core/gxyactor"
)

const (
	ROLE_SERVICE = "role"
)

type roleService struct {
	gxyactor.ActorService
}

func NewRoleGrainService() *roleService {
	return &roleService{}
}

func (r *roleService) ServiceName() string {
	return ROLE_SERVICE
}

func (r *roleService) Weight() int {
	return gxyactor.GetGrainCount(r.ServiceName())
}

func (r *roleService) OnModStart(ctx context.Context) error {
	gxyactor.RegisterGrain(r.ServiceName(), func() gxyactor.IGrain {
		return logic.NewRoleMain()
	})
	return nil
}

func (r *roleService) OnModStop(ctx context.Context) error {
	gxyactor.DeRegisterGrain(r.ServiceName())
	return nil
}
