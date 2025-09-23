package role

import (
	"context"
	"fmt"
	"gserver/core/gxyactor"
	"gserver/core/gxylocator"
	"gserver/core/gxymodule"
	"gserver/core/gxyservice"
	"gserver/service"
	"gserver/service/role/internal/logic"
	"gserver/service/role/roleconsts"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/os/glog"
)

type roleService struct {
	gxymodule.Module
	roleNodeLocator *gxylocator.Locator
}

var roleServiceInstance *roleService

func init() {
	roleServiceInstance = newRoleService()
}

func newRoleService() *roleService {
	return &roleService{
		roleNodeLocator: gxylocator.NewNodeLocator("role", 30*time.Second),
	}
}

func RoleService() *roleService {
	return roleServiceInstance
}

func (s *roleService) Name() string {
	return service.ROLE_SERVICE
}

func (s *roleService) Worker() gxyservice.WorkerCreator {
	return func() gen.ProcessBehavior {
		return logic.NewRoleMain(logic.RoleMainOption{
			Locator: s.roleNodeLocator,
		})
	}
}

func (s *roleService) SpawnRole(roleID int64, reason string) (gen.PID, error) {
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
	rolePid, err := remoteNode.SpawnRegister(gen.Atom(fmt.Sprintf("%s_%d", "role", roleID)), gen.Atom(service.ROLE_SERVICE),
		gen.ProcessOptions{}, reason, roleID)
	if err != nil {
		glog.Errorf(context.Background(), "spawn role error, roleID: %d, err: %v", roleID, err)
		return gen.PID{}, fmt.Errorf("spawn role error: %w", err)
	}
	return rolePid, nil
}

func (s *roleService) GetRoleIDByAccount(account string) (int64, error) {
	return 0, nil
}

func (s *roleService) CallRole(act act.Actor, roleID int64, msg any, spawn bool) (any, error) {
	ctx := context.Background()
	node, locateErr := s.roleNodeLocator.Locate(ctx, fmt.Sprintf("%d", roleID))
	if locateErr != nil {
		return nil, locateErr
	}
	var roleRef any
	if node == "" {
		if spawn {
			rolePid, spawnErr := s.SpawnRole(roleID, roleconsts.ROLE_SPAWN_REASON_LOAD_DATA)
			if spawnErr != nil {
				return nil, spawnErr
			}
			roleRef = rolePid
		}
	} else {
		roleRef = gen.ProcessID{
			Node: gen.Atom(node),
			Name: gen.Atom(s.GetRegName(roleID)),
		}
	}
	rsp, err := act.Call(roleRef, msg)
	if err != nil {
		return nil, err
	}
	return rsp, nil

}

func (s *roleService) GetRegName(roleID int64) string {
	return fmt.Sprintf("%s_%d", "role", roleID)
}
