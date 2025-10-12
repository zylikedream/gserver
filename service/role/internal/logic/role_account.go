package logic

import (
	"context"
	"gserver/core/gxymongo"
	"gserver/util/uid"

	"github.com/gogf/gf/v2/os/glog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type RoleAccount struct {
	RoleID  int64  `bson:"role_id"`
	Account string `bson:"account"`
}

func GetRoleIDByAccount(account string) (int64, error) {
	roleAccount := &RoleAccount{
		Account: account,
	}
	err := gxymongo.Client().FindOne(context.Background(), roleAccount, "role_account", bson.M{"account": account})
	if err != nil && err != mongo.ErrNoDocuments {
		return 0, err
	} else if err == mongo.ErrNoDocuments {
		roleID, err := genRoleID()
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

func GetAccountByRoleID(roleID int64) string {
	roleAccount := &RoleAccount{
		RoleID: roleID,
	}
	err := gxymongo.Client().FindOne(context.Background(), roleAccount, "role_account", bson.M{"role_id": roleID})
	if err != nil {
		if err != mongo.ErrNoDocuments {
			glog.Errorf(context.Background(), "check role exist error, roleID: %d, err: %v", roleID, err)
		}
		return ""
	}
	return roleAccount.Account
}

func genRoleID() (int64, error) {
	var offset int64 = 100000
	uid, err := uid.UidGen().GenAutoIncID("role")
	if err != nil {
		return 0, err
	}
	return uid + offset, nil
}
