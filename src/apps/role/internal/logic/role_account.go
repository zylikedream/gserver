package logic

import (
	"context"
	"errors"

	"gserver/core/gxylog"
	"gserver/core/gxypgx"
	"gserver/src/util/uid"

	"gorm.io/gorm"
)

type RoleAccount struct {
	RoleID  int64  `gorm:"column:role_id;uniqueIndex"`
	Account string `gorm:"column:account;primaryKey"`
}

func (RoleAccount) TableName() string { return "role_account" }

func GetRoleIDByAccount(account string) (int64, error) {
	roleAccount := &RoleAccount{}
	err := gxypgx.DB().Where("account = ?", account).First(roleAccount).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		roleID, err := genRoleID()
		if err != nil {
			return 0, err
		}
		roleAccount.RoleID = roleID
		roleAccount.Account = account
		if err := gxypgx.DB().Save(roleAccount).Error; err != nil {
			return 0, err
		}
	}
	return roleAccount.RoleID, nil
}

func GetAccountByRoleID(roleID int64) string {
	roleAccount := &RoleAccount{}
	err := gxypgx.DB().Where("role_id = ?", roleID).First(roleAccount).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			gxylog.Error(context.Background(), "check role exist error", gxylog.Num("roleID", roleID), gxylog.Err(err))
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
