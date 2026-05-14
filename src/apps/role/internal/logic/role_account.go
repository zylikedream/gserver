package logic

import (
	"context"
	"gserver/core/gxylog"
	"gserver/core/gxypgx"
	"gserver/src/util/uid"
)

type RoleAccount struct {
	RoleID  int64  `gorm:"column:role_id;uniqueIndex"`
	Account string `gorm:"column:account;primaryKey"`
}

func (RoleAccount) TableName() string { return "role_account" }

func GetRoleIDByAccount(ctx context.Context, account string) (int64, error) {
	var accounts []RoleAccount
	gxypgx.DB().Where("account = ?", account).Find(&accounts)
	if len(accounts) > 0 {
		return accounts[0].RoleID, nil
	}
	roleID, err := genRoleID()
	if err != nil {
		return 0, err
	}
	if err := gxypgx.DB().Save(&RoleAccount{RoleID: roleID, Account: account}).Error; err != nil {
		return 0, err
	}
	gxylog.Info(ctx, "create role account", gxylog.Num("roleID", roleID), gxylog.Str("account", account))
	return roleID, nil
}

func GetAccountByRoleID(roleID int64) string {
	var accounts []RoleAccount
	gxypgx.DB().Where("role_id = ?", roleID).Find(&accounts)
	if len(accounts) == 0 {
		return ""
	}
	return accounts[0].Account
}

func genRoleID() (int64, error) {
	var offset int64 = 100000
	uid, err := uid.UidGen().GenAutoIncID("role")
	if err != nil {
		return 0, err
	}
	return uid + offset, nil
}
