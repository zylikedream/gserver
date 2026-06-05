package logic

import (
	"context"

	"gserver/core/gxypgx"
)

func InitAccountSchema(ctx context.Context) error {
	return gxypgx.DB().WithContext(ctx).AutoMigrate(&Account{}, &AccountIdentity{})
}
