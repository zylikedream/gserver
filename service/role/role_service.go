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

func (s *roleService) spawnRole(roleID int64) (gen.PID, error) {
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
		gen.ProcessOptions{}, roleID)
	if err != nil {
		glog.Errorf(context.Background(), "spawn role error, roleID: %d, err: %v", roleID, err)
		return gen.PID{}, fmt.Errorf("spawn role error: %w", err)
	}
	return rolePid, nil
}

func (s *roleService) GetRoleIDByAccount(account string) (int64, error) {
	return 0, nil
}

func (s *roleService) StartRole(roleID int64) (gen.ProcessID, error) {
	ctx := context.Background()
	processID := gen.ProcessID{}
	node, locateErr := s.roleNodeLocator.Locate(ctx, fmt.Sprintf("%d", roleID))
	if locateErr != nil {
		return processID, locateErr
	}
	if node == "" {
		rolePid, spawnErr := s.spawnRole(roleID)
		if spawnErr != nil {
			return processID, spawnErr
		}
		processID = gen.ProcessID{
			Node: rolePid.Node,
			Name: gen.Atom(s.GetRegName(roleID)),
		}
	} else {
		processID = gen.ProcessID{
			Node: gen.Atom(node),
			Name: gen.Atom(s.GetRegName(roleID)),
		}
	}
	return processID, nil
}

func (s *roleService) CallRole(act act.Actor, roleID int64, msg any) (any, error) {
	processID, err := s.StartRole(roleID)
	if err != nil {
		return nil, err
	}
	rsp, err := act.Call(processID, msg)
	if err != nil {
		return nil, err
	}
	return rsp, nil

}

func (s *roleService) GetRegName(roleID int64) string {
	return fmt.Sprintf("%s_%d", "role", roleID)
}
