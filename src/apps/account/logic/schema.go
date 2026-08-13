package logic

import (
	"context"

	"gorm.io/gorm"
)

func InitAccountSchema(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).AutoMigrate(&Account{}, &AccountIdentity{})
}
