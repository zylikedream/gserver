package role

import (
	"context"
	"gserver/apps/role/internal/logic"
	"gserver/core/gxyactor"
	"gserver/core/gxyapp.go"
	"gserver/core/gxyservice"
	"gserver/protocol/pb"
)

const (
	ROLE_SERVICE = "role"
)

type roleApp struct {
	gxyactor.ActorService
	gxyapp.App
}

func NewRoleApp() *roleApp {
	return &roleApp{}
}

func (r *roleApp) ServiceName() string {
	return ROLE_SERVICE
}

func (r *roleApp) Weight() int {
	return gxyactor.ActorSystem().GetGrainCount(r.ServiceName())
}

func (r *roleApp) OnModInit(ctx context.Context) error {
	gxyservice.ServiceApp().LoadService(r)
	r.AddModule(ctx, logic.NewRoleDBIndex())
	return nil
}

func (r *roleApp) OnModStart(ctx context.Context) error {
	gxyactor.ActorSystem().RegisterGrain(r.ServiceName(), func() gxyactor.IGrain {
		return logic.NewRoleMain()
	})
	return nil
}

func (r *roleApp) OnModStop(ctx context.Context) error {
	gxyactor.ActorSystem().DeRegisterGrain(r.ServiceName())
	return nil
}

func GetRolePublic(ctx context.Context, roleID int64) *pb.PRolePublic {
	return logic.GetRolePublic(ctx, roleID)
}

func GetRoleIDByAccount(account string) (int64, error) {
	return logic.GetRoleIDByAccount(account)
}
