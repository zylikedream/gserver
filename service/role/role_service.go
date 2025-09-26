package role

import (
	"gserver/core/gxyactor"
	"gserver/core/gxylocator"
	"gserver/core/gxymodule"
	"gserver/protocol/pb"
	"gserver/service/role/internal/logic"

	"github.com/tochemey/goakt/v3/actor"
)

type roleService struct {
	gxymodule.Module
	roleNodeLocator *gxylocator.Locator
}

func (r *roleService) PreStart(ctx *actor.Context) error {
	return nil
}

func (r *roleService) PostStart(ctx *actor.Context) error {
	return nil
}

func (r *roleService) Recieve(ctx *actor.ReceiveContext) {
	switch ctx.Message().(type) {
	case *pb.SpawnRole:
		// 处理Started消息
	}
}

const (
	RoleServiceName = "role_service"
)

func Name() string {
	return RoleServiceName
}

func (r *roleService) newRoleActor(ctx *actor.ReceiveContext, RoleID int64) gxyactor.PID {
	self := ctx.Self()
	self.SpawnChild(ctx.Context(), GetRegName(RoleID), logic.NewRoleMain())
}
