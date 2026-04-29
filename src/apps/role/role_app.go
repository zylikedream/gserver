package role

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxyapp"
	"gserver/core/gxyservice"
	"gserver/gameconfig"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic"
)

type roleApp struct {
	gxyapp.App
}

func NewRoleApp() *roleApp {
	return &roleApp{}
}

func (r *roleApp) ServiceName() string {
	return ROLE_SERVICE
}

func (r *roleApp) Weight() int {
	return gxyactor.GetActorCount(r.ServiceName())
}

func (r *roleApp) OnModInit(ctx context.Context) error {
	r.AddModule(ctx, gameconfig.NewGameConfig())
	logic.InitRoleSchema(ctx)
	gxyservice.ServiceApp().LoadService(ctx, NewRoleActorService())
	return nil
}

func GetRolePublic(ctx context.Context, roleID int64) *pb.PRolePublic {
	return logic.GetRolePublic(ctx, roleID)
}

func GetRoleIDByAccount(account string) (int64, error) {
	return logic.GetRoleIDByAccount(account)
}
