package logic

import (
	"context"
	"database/sql"
	"errors"
	"gserver/core/gxypgx"
	"gserver/util/uid"

	"github.com/gogf/gf/v2/os/glog"
)

type RoleAccount struct {
	RoleID  int64  `db:"role_id"`
	Account string `db:"account"`
}

func GetRoleIDByAccount(account string) (int64, error) {
	roleAccount := &RoleAccount{
		Account: account,
	}
	err := gxypgx.PGX().FindOne(context.Background(), "role_account", roleAccount, "account=$1", account)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		// Account not found, create new role
		roleID, err := genRoleID()
		if err != nil {
			return 0, err
		}
		roleAccount.RoleID = roleID
		// Use UpsertOne to insert
		err = gxypgx.PGX().UpsertOne(context.Background(), "role_account", roleAccount, "role_id=$1", roleID)
		if err != nil {
			return 0, err
		}
	}
	return roleAccount.RoleID, nil
}

func GetAccountByRoleID(roleID int64) string {
	roleAccount := &RoleAccount{
		RoleID: roleID,
	}
	err := gxypgx.PGX().FindOne(context.Background(), "role_account", roleAccount, "role_id=$1", roleID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
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
