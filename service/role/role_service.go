package role

import (
	"gserver/core/gxyactor"
	"gserver/service/role/internal/logic"

	"github.com/asynkron/protoactor-go/actor"
)

type roleService struct {
}

func (r *roleService) Name() string {
	return "role"
}

func (r *roleService) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		r.Init(ctx)
	default:
	}
}

func (r *roleService) Init(ctx actor.Context) {
	gxyactor.ActorSystem().RegisterGrain(r.Name(), func() actor.Actor {
		return logic.NewRoleMain()
	})
}
