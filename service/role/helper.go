package role

import (
	"context"
	"fmt"
	"gserver/core/gxyactor"
	"gserver/core/gxylocator"
	"gserver/core/gxymongo"
	"gserver/core/gxyregistery"
	"gserver/core/gxyservice"
	"gserver/protocol/pb"
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

type roleServiceHelper struct {
	roleNodeLocator *gxylocator.Locator
}

type RoleAccount struct {
	Account string `bson:"account"`
	RoleID  int64  `bson:"role_id"`
}

var helper *roleServiceHelper

func NewRoleServiceHelper() *roleServiceHelper {
	helper = &roleServiceHelper{
		roleNodeLocator: gxylocator.NewNodeLocator("role", 30*time.Second),
	}
	return helper
}

func RoleServiceHelper() *roleServiceHelper {
	return helper
}

func (s *roleServiceHelper) spawnRole(roleID int64) (string, error) {
	ctx := context.Background()
	roleNode := gxyservice.ServiceModule().GetServiceNode(service.ROLE_SERVICE, gxyregistery.RoundRobinSelector())
	if roleNode.Node == "" {
		glog.Errorf(ctx, "no node for role, roleID: %d", roleID)
		return "", fmt.Errorf("no node for role")
	}
	actorSys := gxyactor.ActorSystem()

	regName := GetRegName(roleID)
	if roleNode.Node == actorSys.GetNodeName() {
		actorSys.Spawn(regName, logic.NewRoleMain(logic.RoleMainOption{
			Locator: s.roleNodeLocator,
		}))
	} else {
		rsp, err := actorSys.GetActorSystem().NoSender().RemoteSpawn(ctx, RoleServiceName, &pb.SpawnRole{
			RoleId: roleID,
		}, 5*time.Second)
		if err != nil {
			return "", err
		}
	}

	return rolePid, nil
}

func (s *roleServiceHelper) OnStart(ctx context.Context) error {

	return nil
}

func (s *roleServiceHelper) GetRoleIDByAccount(account string) (int64, error) {
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

func (s *roleServiceHelper) genRoleID() (int64, error) {
	var offset int64 = 100000
	uid, err := uid.UidGen().GenAutoIncID("role")
	if err != nil {
		return 0, err
	}
	return uid + offset, nil
}

func (s *roleServiceHelper) StartRole(roleID int64) (gen.ProcessID, error) {
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

func (s *roleServiceHelper) CallRole(act act.Actor, roleID int64, msg any) (any, error) {
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
