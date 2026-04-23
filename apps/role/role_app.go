package role

import (
	"context"
	"gserver/apps/role/internal/logic"
	"gserver/core/gxyactor"
	"gserver/core/gxyapp"
	"gserver/core/gxyservice"
	"gserver/protocol/pb"
)

type roleApp struct {
	gxyapp.App
}

func NewRoleApp() *roleApp {
	return &roleApp{}
}

func (r *roleApp) Deps() []string {
	return []string{"redis", "pgx", "actor", "service"}
}

func (r *roleApp) ServiceName() string {
	return ROLE_SERVICE
}

func (r *roleApp) Weight() int {
	return gxyactor.GetActorCount(r.ServiceName())
}

func (r *roleApp) OnModInit(ctx context.Context) error {
	// 初始化 PostgreSQL 表结构
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
