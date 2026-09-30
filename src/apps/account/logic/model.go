package logic

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gserver/core/gxylog"
	"gserver/src/util/uid"

	"github.com/cockroachdb/errors"

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

// accountStore 账号持久化。id 分配属于落库的一部分(role_id 来自 PG sequence),
// 因此由实现方生成——测试注入内存实现时同样由它分配,无需为测试留可写入口。
type accountStore interface {
	FindAccountByIdentity(ctx context.Context, platform string, platformUID string) (*Account, error)
	CreateAccount(ctx context.Context, platform string, platformUID string) (*Account, error)
}

type gormAccountStore struct {
	db *gorm.DB
}

func (s gormAccountStore) FindAccountByIdentity(ctx context.Context, platform string, platformUID string) (*Account, error) {
	var account Account
	err := s.db.WithContext(ctx).
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

// CreateAccount 分配 account_id 与 role_id,把 account 与 identity 落在同一事务里。
func (s gormAccountStore) CreateAccount(ctx context.Context, platform string, platformUID string) (*Account, error) {
	account := &Account{AccountID: fmt.Sprintf("acc_%s", uid.RandomStrID())}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// role_id 由 PG sequence 生成:持久、原子、多实例安全。
		roleID, err := uid.NewGen(tx).GenAutoIncID("role")
		if err != nil {
			return err
		}
		account.RoleID = roleID
		if err := tx.Create(account).Error; err != nil {
			return err
		}
		return tx.Create(&AccountIdentity{
			Platform:    platform,
			PlatformUID: platformUID,
			AccountID:   account.AccountID,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return account, nil
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

// Service 承载 account 业务依赖:组装根注入,测试注入内存 store 与固定时钟
// (见 invariants.md「测试替身规则」)。
type Service struct {
	store accountStore
	clock func() time.Time
}

// NewService 用真实存储构造业务服务。
func NewService(db *gorm.DB) *Service {
	return &Service{
		store: gormAccountStore{db: db},
		clock: time.Now,
	}
}

func (s *Service) LoadAccountByIdentity(ctx context.Context, platform string, platformUID string) (*Account, error) {
	if platform == "" || platformUID == "" {
		return nil, gerror.New("platform and platform_uid are required")
	}
	return s.store.FindAccountByIdentity(ctx, platform, platformUID)
}

// CreateAccountWithIdentity 首次登录建号;已存在则返回既有记录。返回的 isNew 表示本次是否新建。
func (s *Service) CreateAccountWithIdentity(ctx context.Context, platform string, platformUID string) (*Account, bool, error) {
	if platform == "" || platformUID == "" {
		return nil, false, gerror.New("platform and platform_uid are required")
	}

	existing, err := s.LoadAccountByIdentity(ctx, platform, platformUID)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, false, nil
	}

	account, err := s.store.CreateAccount(ctx, platform, platformUID)
	if err != nil {
		if isUniqueConstraintError(err) {
			reloaded, reloadErr := s.LoadAccountByIdentity(ctx, platform, platformUID)
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
