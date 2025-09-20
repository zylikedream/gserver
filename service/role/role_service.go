package role

import (
	"context"
	"fmt"
	"gserver/core/gxyactor"
	"gserver/core/gxyservice"
	"gserver/service"
	"gserver/service/role/internal/logic"

	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/os/glog"
)

type roleService struct {
}

func RoleService() *roleService {
	return &roleService{}
}

func (s *roleService) Name() string {
	return service.ROLE_SERVICE
}

func (s *roleService) Worker() gxyservice.WorkerCreator {
	return func() gen.ProcessBehavior {
		return logic.NewRoleMain()
	}
}

func (s *roleService) GetRolePid(roleID uint64, spawn bool) (gen.PID, error) {
	roleNode := gxyservice.ServiceModule().GetServiceNode(service.ROLE_SERVICE, gxyservice.RoundRobinSelector())
	if roleNode.Node == "" {
		glog.Errorf(context.Background(), "no node for role, roleID: %d", roleID)
		return gen.PID{}, fmt.Errorf("no node for role")
	}
	remoteNode, err := gxyactor.ActorSystem().GetRemoteNode(roleNode.Node)
	if err != nil {
		glog.Errorf(context.Background(), "get node error, roleID: %d, err: %v", roleID, err)
		return gen.PID{}, fmt.Errorf("get node error: %w", err)
	}

	rolePid, err := remoteNode.Spawn(gen.Atom(service.ROLE_SERVICE), gen.ProcessOptions{LinkParent: true})
	if err != nil {
		glog.Errorf(context.Background(), "spawn role error, roleID: %d, err: %v", roleID, err)
		return gen.PID{}, fmt.Errorf("spawn role error: %w", err)
	}
	return rolePid, nil
}
