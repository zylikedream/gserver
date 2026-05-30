package logic

import (
	"context"
	"errors"
	"testing"
)

type inMemoryAccountStore struct {
	accountsByID          map[string]*Account
	accountsByRoleID      map[int64]*Account
	identitiesByPlatform  map[string]*AccountIdentity
}

func newInMemoryAccountStore() *inMemoryAccountStore {
	return &inMemoryAccountStore{
		accountsByID:         make(map[string]*Account),
		accountsByRoleID:     make(map[int64]*Account),
		identitiesByPlatform: make(map[string]*AccountIdentity),
	}
}

func (s *inMemoryAccountStore) FindAccountByIdentity(_ context.Context, platform string, platformUID string) (*Account, error) {
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

func (s *inMemoryAccountStore) CreateAccountWithIdentity(_ context.Context, account *Account, identity *AccountIdentity) error {
	key := identity.Platform + ":" + identity.PlatformUID
	if _, ok := s.identitiesByPlatform[key]; ok {
		return errors.New("duplicate key value violates unique constraint")
	}
	if _, ok := s.accountsByID[account.AccountID]; ok {
		return errors.New("duplicate key value violates unique constraint")
	}
	if _, ok := s.accountsByRoleID[account.RoleID]; ok {
		return errors.New("duplicate key value violates unique constraint")
	}
	accountClone := *account
	identityClone := *identity
	s.accountsByID[account.AccountID] = &accountClone
	s.accountsByRoleID[account.RoleID] = &accountClone
	s.identitiesByPlatform[key] = &identityClone
	return nil
}

type uniquenessConflictAccountStore struct {
	account *Account
}

func (s *uniquenessConflictAccountStore) FindAccountByIdentity(_ context.Context, platform string, platformUID string) (*Account, error) {
	if s.account == nil {
		return nil, nil
	}
	cloned := *s.account
	return &cloned, nil
}

func (s *uniquenessConflictAccountStore) CreateAccountWithIdentity(_ context.Context, _ *Account, identity *AccountIdentity) error {
	s.account = &Account{
		AccountID: "acc_raced",
		RoleID:    10077,
	}
	if identity.Platform == "" || identity.PlatformUID == "" {
		return errors.New("missing identity")
	}
	return errors.New("duplicate key value violates unique constraint")
}

func swapAccountStore(t *testing.T, store accountStore) {
	t.Helper()
	oldStore := accounts
	accounts = store
	t.Cleanup(func() {
		accounts = oldStore
	})
}

func TestLoadAccountByIdentityReturnsNilWhenMissing(t *testing.T) {
	store := newInMemoryAccountStore()
	swapAccountStore(t, store)

	account, err := LoadAccountByIdentity(context.Background(), "guest", "u_missing")
	if err != nil {
		t.Fatalf("load account by identity failed: %v", err)
	}
	if account != nil {
		t.Fatalf("expected nil account, got %+v", account)
	}
}

func TestCreateAccountWithIdentityCreatesAccountAndIdentity(t *testing.T) {
	store := newInMemoryAccountStore()
	swapAccountStore(t, store)
	swapIDGenerators(t, "acc_1001", 10001)

	account, isNew, err := CreateAccountWithIdentity(context.Background(), "guest", "u_1001")
	if err != nil {
		t.Fatalf("create account with identity failed: %v", err)
	}
	if !isNew || account.AccountID != "acc_1001" || account.RoleID != 10001 {
		t.Fatalf("unexpected account: %+v", account)
	}
}

func TestCreateAccountWithIdentityReturnsExistingAccount(t *testing.T) {
	store := newInMemoryAccountStore()
	swapAccountStore(t, store)
	swapIDGenerators(t, "acc_1002", 10002)

	first, _, err := CreateAccountWithIdentity(context.Background(), "guest", "u_1002")
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	second, isNew, err := CreateAccountWithIdentity(context.Background(), "guest", "u_1002")
	if err != nil {
		t.Fatalf("second create failed: %v", err)
	}
	if isNew || first.AccountID != second.AccountID || first.RoleID != second.RoleID {
		t.Fatalf("expected existing account reuse, first=%+v second=%+v", first, second)
	}
}

func TestCreateAccountWithIdentityReloadsAfterUniquenessConflict(t *testing.T) {
	store := &uniquenessConflictAccountStore{}
	swapAccountStore(t, store)
	swapIDGenerators(t, "acc_1003", 10003)

	account, isNew, err := CreateAccountWithIdentity(context.Background(), "guest", "u_1003")
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
