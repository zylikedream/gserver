package role

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxymongo"
	"gserver/service"
	"gserver/service/role/internal/logic"
	"gserver/util/uid"
	"strconv"

	"github.com/asynkron/protoactor-go/actor"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
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

func (r *roleService) OnStart(ctx context.Context) error {
	gxyactor.ActorSystem().RegisterGrain(r.Name(), func() actor.Actor {
		return logic.NewRoleMain()
	})
	return nil
}

func (s *roleService) GetRoleIDByAccount(account string) (int64, error) {
	roleAccount := &RoleAccount{
		Account: account,
	}
	err := gxymongo.Client().FindOne(context.Background(), &roleAccount, "role_account", bson.M{"account": account})
	if err != nil && err != mongo.ErrNoDocuments {
		return 0, err
	} else if err == mongo.ErrNoDocuments {
		roleID, err := s.genRoleID()
		if err != nil {
			return 0, err
		}
		roleAccount.RoleID = roleID
		if _, err := gxymongo.Client().InsertOne(context.Background(), "role_account", roleAccount); err != nil {
			return 0, err
		}
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

func (s *roleService) GetRole(roleID int64) (gxyactor.PID, error) {
	pid, err := gxyactor.ActorSystem().GetGrain(s.Name(), strconv.Itoa(int(roleID)))
	if err != nil {
		return nil, err
	}
	return pid, nil
}

type RoleAccount struct {
	Account string `bson:"account"`
	RoleID  int64  `bson:"roleID"`
}
