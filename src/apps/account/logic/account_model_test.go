package logic

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// inMemoryAccountStore 内存实现:连接与 id 分配都由实现方负责,
// 因此调用方(Service)不需要为测试留任何可写入口。
type inMemoryAccountStore struct {
	accountsByID         map[string]*Account
	accountsByRoleID     map[int64]*Account
	identitiesByPlatform map[string]*AccountIdentity
	seq                  int
	createErr            error
	findErr              error
}

func newInMemoryAccountStore() *inMemoryAccountStore {
	return &inMemoryAccountStore{
		accountsByID:         make(map[string]*Account),
		accountsByRoleID:     make(map[int64]*Account),
		identitiesByPlatform: make(map[string]*AccountIdentity),
	}
}

// 固定 id 规则:第 n 次建号 → acc-test-<n> / role 10000+n。
func (s *inMemoryAccountStore) accountForSeq() *Account {
	return &Account{AccountID: fmt.Sprintf("acc-test-%d", s.seq), RoleID: 10000 + int64(s.seq)}
}

func (s *inMemoryAccountStore) FindAccountByIdentity(_ context.Context, platform string, platformUID string) (*Account, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	identity, ok := s.identitiesByPlatform[platform+":"+platformUID]
	if !ok {
		return nil, nil
	}
	account, ok := s.accountsByID[identity.AccountID]
	if !ok {
		return nil, nil
	}
	cloned := *account
	return &cloned, nil
}

func (s *inMemoryAccountStore) CreateAccount(_ context.Context, platform string, platformUID string) (*Account, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	s.seq++
	account := s.accountForSeq()
	identity := &AccountIdentity{Platform: platform, PlatformUID: platformUID, AccountID: account.AccountID}
	key := platform + ":" + platformUID
	if _, ok := s.identitiesByPlatform[key]; ok {
		return nil, errors.New("duplicate key value violates unique constraint")
	}
	accountClone := *account
	identityClone := *identity
	s.accountsByID[account.AccountID] = &accountClone
	s.accountsByRoleID[account.RoleID] = &accountClone
	s.identitiesByPlatform[key] = &identityClone
	return &accountClone, nil
}

// uniquenessConflictAccountStore 建号时撞唯一约束:首次查无、写入冲突,重新查询才拿到既有记录。
type uniquenessConflictAccountStore struct {
	account   *Account
	reloadErr error
	attempted bool
}

func (s *uniquenessConflictAccountStore) FindAccountByIdentity(_ context.Context, _ string, _ string) (*Account, error) {
	if !s.attempted {
		return nil, nil
	}
	if s.reloadErr != nil {
		return nil, s.reloadErr
	}
	cloned := *s.account
	return &cloned, nil
}

func (s *uniquenessConflictAccountStore) CreateAccount(_ context.Context, _ string, _ string) (*Account, error) {
	s.attempted = true
	s.account = &Account{AccountID: "acc_raced", RoleID: 10077}
	return nil, errors.New("duplicate key value violates unique constraint")
}

// newTestService 构造注入了内存 store 与固定时钟的业务服务。
func newTestService(store accountStore) *Service {
	return &Service{
		store: store,
		clock: func() time.Time { return time.Unix(1710000000, 0) },
	}
}

func TestLoadAccountByIdentityReturnsNilWhenMissing(t *testing.T) {
	svc := newTestService(newInMemoryAccountStore())

	account, err := svc.LoadAccountByIdentity(context.Background(), "guest", "u_missing")
	if err != nil {
		t.Fatalf("load account by identity failed: %v", err)
	}
	if account != nil {
		t.Fatalf("expected nil account, got %+v", account)
	}
}

func TestCreateAccountWithIdentityCreatesAccountAndIdentity(t *testing.T) {
	store := newInMemoryAccountStore()
	svc := newTestService(store)

	account, isNew, err := svc.CreateAccountWithIdentity(context.Background(), "guest", "u_1001")
	if err != nil {
		t.Fatalf("create account with identity failed: %v", err)
	}
	if !isNew || account.AccountID != "acc-test-1" || account.RoleID != 10001 {
		t.Fatalf("unexpected account: %+v", account)
	}
	if _, ok := store.identitiesByPlatform["guest:u_1001"]; !ok {
		t.Fatal("identity not persisted")
	}
}

func TestCreateAccountWithIdentityReturnsExistingAccount(t *testing.T) {
	svc := newTestService(newInMemoryAccountStore())

	first, _, err := svc.CreateAccountWithIdentity(context.Background(), "guest", "u_1002")
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	second, isNew, err := svc.CreateAccountWithIdentity(context.Background(), "guest", "u_1002")
	if err != nil {
		t.Fatalf("second create failed: %v", err)
	}
	if isNew || first.AccountID != second.AccountID || first.RoleID != second.RoleID {
		t.Fatalf("expected existing account reuse, first=%+v second=%+v", first, second)
	}
}

func TestCreateAccountWithIdentityReloadsAfterUniquenessConflict(t *testing.T) {
	svc := newTestService(&uniquenessConflictAccountStore{})

	account, isNew, err := svc.CreateAccountWithIdentity(context.Background(), "guest", "u_1003")
	if err != nil {
		t.Fatalf("create account with identity failed: %v", err)
	}
	if isNew {
		t.Fatalf("expected raced create to reload existing account")
	}
	if account.AccountID != "acc_raced" || account.RoleID != 10077 {
		t.Fatalf("unexpected reloaded account: %+v", account)
	}
}

func TestAccountIdentityRequiresBothPlatformFields(t *testing.T) {
	svc := newTestService(newInMemoryAccountStore())

	for _, test := range []struct {
		name        string
		platform    string
		platformUID string
	}{
		{name: "missing platform", platformUID: "uid-1"},
		{name: "missing platform uid", platform: "guest"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if account, err := svc.LoadAccountByIdentity(context.Background(), test.platform, test.platformUID); err == nil || account != nil {
				t.Fatalf("load should reject incomplete identity, account=%+v err=%v", account, err)
			}
			if account, isNew, err := svc.CreateAccountWithIdentity(context.Background(), test.platform, test.platformUID); err == nil || account != nil || isNew {
				t.Fatalf("create should reject incomplete identity, account=%+v isNew=%v err=%v", account, isNew, err)
			}
		})
	}
}

func TestCreateAccountWithIdentityPropagatesDependencyErrors(t *testing.T) {
	lookupErr := errors.New("lookup unavailable")
	createErr := errors.New("store unavailable")
	reloadErr := errors.New("reload unavailable")

	t.Run("initial lookup", func(t *testing.T) {
		svc := newTestService(&inMemoryAccountStore{findErr: lookupErr})
		_, _, err := svc.CreateAccountWithIdentity(context.Background(), "guest", "uid-lookup")
		if !errors.Is(err, lookupErr) {
			t.Fatalf("expected lookup error, got %v", err)
		}
	})

	t.Run("store create", func(t *testing.T) {
		svc := newTestService(&inMemoryAccountStore{createErr: createErr})
		_, _, err := svc.CreateAccountWithIdentity(context.Background(), "guest", "uid-store")
		if !errors.Is(err, createErr) {
			t.Fatalf("expected store error, got %v", err)
		}
	})

	t.Run("reload after uniqueness conflict", func(t *testing.T) {
		svc := newTestService(&uniquenessConflictAccountStore{reloadErr: reloadErr})
		_, _, err := svc.CreateAccountWithIdentity(context.Background(), "guest", "uid-reload")
		if !errors.Is(err, reloadErr) {
			t.Fatalf("expected reload error, got %v", err)
		}
	})
}
