package role

import (
	"context"
	"fmt"
	"gserver/core/gxyactor"
	"gserver/core/gxylocator"
	"gserver/core/gxymodule"
	"gserver/core/gxymongo"
	"gserver/core/gxyservice"
	"gserver/service"
	"gserver/service/role/internal/logic"
	"gserver/util/uid"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/os/glog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type roleService struct {
	gxymodule.Module
	roleNodeLocator *gxylocator.Locator
}

type RoleAccount struct {
	Account string `bson:"account"`
	RoleID  int64  `bson:"role_id"`
}

var roleServiceInstance *roleService

func NewRoleService() *roleService {
	roleServiceInstance = &roleService{
		roleNodeLocator: gxylocator.NewNodeLocator("role", 30*time.Second),
	}
	return roleServiceInstance
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

func (s *roleService) IsRemote() bool {
	return true
}

func (s *roleService) spawnRole(roleID int64) (gen.PID, error) {
	roleNode := gxyservice.ServiceModule().GetServiceNode(service.ROLE_SERVICE, gxyservice.RoundRobinSelector())
	if roleNode.Node == "" {
		glog.Errorf(context.Background(), "no node for role, roleID: %d", roleID)
		return gen.PID{}, fmt.Errorf("no node for role")
	}
	actorSys := gxyactor.ActorSystem()
	if roleNode.Node == actorSys.GetNodeName() {
		return actorSys.SpawnRegister(GetRegName(roleID), gen.ProcessFactory(s.Worker()),
			gen.ProcessOptions{}, roleID)
	}
	remoteNode, err := actorSys.GetRemoteNode(roleNode.Node)
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

func (s *roleService) OnStart(ctx context.Context) error {

	return nil
}

func (s *roleService) GetRoleIDByAccount(account string) (int64, error) {
	roleAccount := &RoleAccount{
		Account: account,
	}
	err := gxymongo.Client().FindOne(context.Background(), &roleAccount, "role_account", bson.M{"account": account})
	if err != nil && err != mongo.ErrNoDocuments {
		return 0, err
	} else {
		roleID, err := s.genRoleID()
		if err != nil {
			return 0, err
		}
		if _, err := gxymongo.Client().InsertOne(context.Background(), "role_account", roleAccount); err != nil {
			return 0, err
		}
		roleAccount.RoleID = roleID
	}
	return roleAccount.RoleID, nil
}

func (s *roleService) genRoleID() (int64, error) {
	var offset int64 = 100000
	uid, err := uid.UidGen().GenAutoIncID("role")
	if err != nil {
		return 0, err
	}
	return uid + offset, nil
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
			Name: gen.Atom(GetRegName(roleID)),
		}
	} else {
		processID = gen.ProcessID{
			Node: gen.Atom(node),
			Name: gen.Atom(GetRegName(roleID)),
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

func GetRegName(roleID int64) string {
	return fmt.Sprintf("%s_%d", "role", roleID)
}
