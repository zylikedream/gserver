package role

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/src/apps/role/internal/logic"
	"gserver/src/lib"
)

type roleService struct {
	gxyactor.ActorService
}

func NewRoleActorService() *roleService {
	return &roleService{}
}

func (r *roleService) ServiceName() string {
	return lib.ROLE_ACTOR_TYPE
}

func (r *roleService) Weight() int {
	return gxyactor.GetActorCount(r.ServiceName())
}

func (r *roleService) OnModStart(ctx context.Context) error {
	if err := gxyactor.RegisterActorKind(r.ServiceName(), func() gxyactor.IActor {
		return logic.NewRoleMain()
	}); err != nil {
		return err
	}
	return nil
}

func (r *roleService) OnModStop(ctx context.Context) error {
	gxyactor.DeregisterActorKind(r.ServiceName())
	return nil
}
