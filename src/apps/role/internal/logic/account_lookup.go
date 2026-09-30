package logic

import (
	"context"

	"github.com/cockroachdb/errors"

	accountlogic "gserver/src/apps/account/logic"

	"gorm.io/gorm"
)

// 账号反查:连接从接收者取(RoleMain.DB),测试经 deps 注入 sqlmock。

func (r *RoleMain) lookupAccountIDByRoleID(ctx context.Context) (string, error) {
	account, err := r.loadAccountByRoleID(ctx)
	if err != nil {
		return "", err
	}
	if account == nil {
		return "", nil
	}
	return account.AccountID, nil
}

func (r *RoleMain) loadAccountByRoleID(ctx context.Context) (*accountlogic.Account, error) {
	var account accountlogic.Account
	err := r.DB().WithContext(ctx).
		Where("role_id = ?", r.RoleID).
		First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &account, nil
}
