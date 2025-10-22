package role

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/service"
	"gserver/service/role/internal/logic"
	"strconv"
)

type roleService struct {
	gxyactor.Service
}

var roleSvc = newRoleService()

func RoleService() *roleService {
	return roleSvc
}

func newRoleService() *roleService {
	return &roleService{}
}

func (r *roleService) Name() string {
	return service.ROLE_SERVICE
}

func (r *roleService) Weight() int {
	return gxyactor.ActorSystem().GetGrainCount(r.Name())
}

func (r *roleService) OnModInit(ctx context.Context) error {
	return nil
}

func (r *roleService) OnModStart(ctx context.Context) error {
	gxyactor.ActorSystem().RegisterGrain(r.Name(), func() gxyactor.IGrain {
		return logic.NewRoleMain()
	})
	return nil
}

func (r *roleService) OnModStop(ctx context.Context) error {
	gxyactor.ActorSystem().DeRegisterGrain(r.Name())
	return nil
}

func (s *roleService) GetRole(roleID int64) (gxyactor.PID, error) {
	pid, err := gxyactor.ActorSystem().GetGrain(s.Name(), strconv.Itoa(int(roleID)))
	if err != nil {
		return nil, err
	}
	return pid, nil
}

func GetRoleIDByAccount(account string) (int64, error) {
	return logic.GetRoleIDByAccount(account)
}
