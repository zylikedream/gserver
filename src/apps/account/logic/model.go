package logic

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxypgx"
	"gserver/src/util/uid"

	"github.com/gogf/gf/v2/errors/gerror"
	"gorm.io/gorm"
)

type AccountMapping struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Platform    string    `gorm:"column:platform;size:32;not null;uniqueIndex:uk_platform_uid"`
	PlatformUID string    `gorm:"column:platform_uid;size:128;not null;uniqueIndex:uk_platform_uid"`
	AccountID   string    `gorm:"column:account_id;size:64;not null;uniqueIndex"`
	RoleID      int64     `gorm:"column:role_id;not null;uniqueIndex"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (AccountMapping) TableName() string {
	return "account_mapping"
}

type legacyRoleAccount struct {
	RoleID  int64  `gorm:"column:role_id;uniqueIndex"`
	Account string `gorm:"column:account;primaryKey"`
}

func (legacyRoleAccount) TableName() string {
	return "role_account"
}

type accountMappingStore interface {
	FindByPlatformUID(ctx context.Context, platform string, platformUID string) (*AccountMapping, error)
	Create(ctx context.Context, mapping *AccountMapping) error
}

type gormAccountMappingStore struct{}

var (
	accountMappings  accountMappingStore = gormAccountMappingStore{}
	generateAccountID                    = defaultGenerateAccountID
	generateRoleID                       = defaultGenerateRoleID
	ensureLegacyRoleAccount              = defaultEnsureLegacyRoleAccount
)

func LoadOrCreateAccountMapping(ctx context.Context, platform string, platformUID string) (*AccountMapping, bool, error) {
	if platform == "" || platformUID == "" {
		return nil, false, gerror.New("platform and platform_uid are required")
	}

	existing, err := accountMappings.FindByPlatformUID(ctx, platform, platformUID)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, false, nil
	}

	accountID, err := generateAccountID()
	if err != nil {
		return nil, false, err
	}
	roleID, err := generateRoleID()
	if err != nil {
		return nil, false, err
	}

	mapping := &AccountMapping{
		Platform:    platform,
		PlatformUID: platformUID,
		AccountID:   accountID,
		RoleID:      roleID,
	}
	if err := accountMappings.Create(ctx, mapping); err != nil {
		if isUniqueConstraintError(err) {
			reloaded, reloadErr := accountMappings.FindByPlatformUID(ctx, platform, platformUID)
			if reloadErr != nil {
				return nil, false, reloadErr
			}
			if reloaded != nil {
				return reloaded, false, nil
			}
		}
		return nil, false, err
	}
	if err := ensureLegacyRoleAccount(ctx, mapping); err != nil {
		return nil, false, err
	}

	gxylog.Info(ctx, "create account mapping",
		gxylog.Str("platform", platform),
		gxylog.Str("platform_uid", platformUID),
		gxylog.Str("account_id", mapping.AccountID),
		gxylog.Num("role_id", mapping.RoleID),
	)
	return mapping, true, nil
}

func (gormAccountMappingStore) FindByPlatformUID(ctx context.Context, platform string, platformUID string) (*AccountMapping, error) {
	var mapping AccountMapping
	err := gxypgx.DB().WithContext(ctx).
		Where("platform = ? AND platform_uid = ?", platform, platformUID).
		First(&mapping).Error
	if err == nil {
		return &mapping, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return nil, err
}

func (gormAccountMappingStore) Create(ctx context.Context, mapping *AccountMapping) error {
	return gxypgx.DB().WithContext(ctx).Create(mapping).Error
}

func defaultGenerateAccountID() (string, error) {
	return fmt.Sprintf("acc_%s", uid.UidGen().GenRandomStrID()), nil
}

func defaultGenerateRoleID() (int64, error) {
	const offset int64 = 100000
	id, err := uid.UidGen().GenAutoIncID("role")
	if err != nil {
		return 0, err
	}
	return id + offset, nil
}

func defaultEnsureLegacyRoleAccount(ctx context.Context, mapping *AccountMapping) error {
	return gxypgx.DB().WithContext(ctx).Save(&legacyRoleAccount{
		RoleID:  mapping.RoleID,
		Account: mapping.AccountID,
	}).Error
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	lowered := strings.ToLower(err.Error())
	return strings.Contains(lowered, "duplicate") || strings.Contains(lowered, "unique")
}
