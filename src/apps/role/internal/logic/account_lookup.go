package logic

import (
	"context"
	"errors"

	accountlogic "gserver/src/apps/account/logic"

	"gserver/core/gxypgx"

	"gorm.io/gorm"
)

var (
	lookupAccountIDByRoleID = defaultLookupAccountIDByRoleID
	loadAccountByRoleID     = defaultLoadAccountByRoleID
)

func defaultLookupAccountIDByRoleID(ctx context.Context, roleID int64) (string, error) {
	account, err := loadAccountByRoleID(ctx, roleID)
	if err != nil {
		return "", err
	}
	if account == nil {
		return "", nil
	}
	return account.AccountID, nil
}

func defaultLoadAccountByRoleID(ctx context.Context, roleID int64) (*accountlogic.Account, error) {
	var account accountlogic.Account
	err := gxypgx.DB().WithContext(ctx).
		Where("role_id = ?", roleID).
		First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &account, nil
}
