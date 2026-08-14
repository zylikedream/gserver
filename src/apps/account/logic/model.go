package logic

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/errors"

	"gserver/core/gxylog"
	"gserver/core/gxypgx"
	"gserver/src/util/uid"

	"github.com/gogf/gf/v2/errors/gerror"
	"gorm.io/gorm"
)

type Account struct {
	AccountID string    `gorm:"column:account_id;primaryKey"`
	RoleID    int64     `gorm:"column:role_id;not null;uniqueIndex"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (Account) TableName() string {
	return "account"
}

type AccountIdentity struct {
	Platform    string    `gorm:"column:platform;primaryKey"`
	PlatformUID string    `gorm:"column:platform_uid;primaryKey"`
	AccountID   string    `gorm:"column:account_id;not null;index"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (AccountIdentity) TableName() string {
	return "account_identity"
}

type accountStore interface {
	FindAccountByIdentity(ctx context.Context, platform string, platformUID string) (*Account, error)
	CreateAccountWithIdentity(ctx context.Context, account *Account, identity *AccountIdentity) error
}

type gormAccountStore struct {
	// db 惰性获取:包级初始化不触碰全局,方法调用时才取(测试可注入)。
	db func() *gorm.DB
}

var (
	accounts          accountStore = gormAccountStore{db: gxypgx.DB}
	generateAccountID              = defaultGenerateAccountID
	generateRoleID                 = defaultGenerateRoleID
)

func LoadAccountByIdentity(ctx context.Context, platform string, platformUID string) (*Account, error) {
	if platform == "" || platformUID == "" {
		return nil, gerror.New("platform and platform_uid are required")
	}
	return accounts.FindAccountByIdentity(ctx, platform, platformUID)
}

func CreateAccountWithIdentity(ctx context.Context, platform string, platformUID string) (*Account, bool, error) {
	if platform == "" || platformUID == "" {
		return nil, false, gerror.New("platform and platform_uid are required")
	}

	existing, err := LoadAccountByIdentity(ctx, platform, platformUID)
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

	account := &Account{
		AccountID: accountID,
		RoleID:    roleID,
	}
	identity := &AccountIdentity{
		Platform:    platform,
		PlatformUID: platformUID,
		AccountID:   accountID,
	}
	if err := accounts.CreateAccountWithIdentity(ctx, account, identity); err != nil {
		if isUniqueConstraintError(err) {
			reloaded, reloadErr := LoadAccountByIdentity(ctx, platform, platformUID)
			if reloadErr != nil {
				return nil, false, reloadErr
			}
			if reloaded != nil {
				return reloaded, false, nil
			}
		}
		return nil, false, err
	}

	gxylog.Info(ctx, "create account",
		gxylog.Str("platform", platform),
		gxylog.Str("platform_uid", platformUID),
		gxylog.Str("account_id", account.AccountID),
		gxylog.Num("role_id", account.RoleID),
	)
	return account, true, nil
}

func (s gormAccountStore) FindAccountByIdentity(ctx context.Context, platform string, platformUID string) (*Account, error) {
	var account Account
	err := s.db().WithContext(ctx).
		Table(account.TableName()).
		Select("account.*").
		Joins("JOIN account_identity ON account_identity.account_id = account.account_id").
		Where("account_identity.platform = ? AND account_identity.platform_uid = ?", platform, platformUID).
		First(&account).Error
	if err == nil {
		return &account, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return nil, err
}

func (s gormAccountStore) CreateAccountWithIdentity(ctx context.Context, account *Account, identity *AccountIdentity) error {
	return s.db().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(account).Error; err != nil {
			return err
		}
		if err := tx.Create(identity).Error; err != nil {
			return err
		}
		return nil
	})
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

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	lowered := strings.ToLower(err.Error())
	return strings.Contains(lowered, "duplicate") || strings.Contains(lowered, "unique")
}
