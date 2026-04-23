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

func NewRoleActorService() *roleService {
	return &roleService{}
}

func (r *roleService) ServiceName() string {
	return ROLE_SERVICE
}

func (r *roleService) Weight() int {
	return gxyactor.GetActorCount(r.ServiceName())
}

func (r *roleService) OnModStart(ctx context.Context) error {
	gxyactor.RegisterActorKind(r.ServiceName(), func() gxyactor.IActor {
		return logic.NewRoleMain()
	})
	return nil
}

func (r *roleService) OnModStop(ctx context.Context) error {
	gxyactor.DeregisterActorKind(r.ServiceName())
	return nil
}
