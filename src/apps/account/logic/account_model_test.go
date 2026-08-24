package logic

import (
	"context"
	"errors"
	"testing"
)

type inMemoryAccountStore struct {
	accountsByID         map[string]*Account
	accountsByRoleID     map[int64]*Account
	identitiesByPlatform map[string]*AccountIdentity
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

type errorAccountStore struct {
	findErr   error
	createErr error
}

func (s errorAccountStore) FindAccountByIdentity(context.Context, string, string) (*Account, error) {
	return nil, s.findErr
}

func (s errorAccountStore) CreateAccountWithIdentity(context.Context, *Account, *AccountIdentity) error {
	return s.createErr
}

type uniquenessReloadErrorStore struct {
	createAttempted bool
	reloadErr       error
}

func (s *uniquenessReloadErrorStore) FindAccountByIdentity(context.Context, string, string) (*Account, error) {
	if s.createAttempted {
		return nil, s.reloadErr
	}
	return nil, nil
}

func (s *uniquenessReloadErrorStore) CreateAccountWithIdentity(context.Context, *Account, *AccountIdentity) error {
	s.createAttempted = true
	return errors.New("duplicate key value violates unique constraint")
}

func swapIDGeneratorFuncs(t *testing.T, accountID func() (string, error), roleID func() (int64, error)) {
	t.Helper()
	oldAccountID := generateAccountID
	oldRoleID := generateRoleID
	generateAccountID = accountID
	generateRoleID = roleID
	t.Cleanup(func() {
		generateAccountID = oldAccountID
		generateRoleID = oldRoleID
	})
}

func TestAccountIdentityRequiresBothPlatformFields(t *testing.T) {
	swapAccountStore(t, newInMemoryAccountStore())

	for _, test := range []struct {
		name        string
		platform    string
		platformUID string
	}{
		{name: "missing platform", platformUID: "uid-1"},
		{name: "missing platform uid", platform: "guest"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if account, err := LoadAccountByIdentity(context.Background(), test.platform, test.platformUID); err == nil || account != nil {
				t.Fatalf("load should reject incomplete identity, account=%+v err=%v", account, err)
			}
			if account, isNew, err := CreateAccountWithIdentity(context.Background(), test.platform, test.platformUID); err == nil || account != nil || isNew {
				t.Fatalf("create should reject incomplete identity, account=%+v isNew=%v err=%v", account, isNew, err)
			}
		})
	}
}

func TestCreateAccountWithIdentityPropagatesDependencyErrors(t *testing.T) {
	lookupErr := errors.New("lookup unavailable")
	accountIDErr := errors.New("account id unavailable")
	roleIDErr := errors.New("role id unavailable")
	storeErr := errors.New("store unavailable")
	reloadErr := errors.New("reload unavailable")

	t.Run("initial lookup", func(t *testing.T) {
		swapAccountStore(t, errorAccountStore{findErr: lookupErr})
		swapIDGeneratorFuncs(t,
			func() (string, error) { t.Fatal("account ID generator must not run"); return "", nil },
			func() (int64, error) { t.Fatal("role ID generator must not run"); return 0, nil },
		)
		_, _, err := CreateAccountWithIdentity(context.Background(), "guest", "uid-lookup")
		if !errors.Is(err, lookupErr) {
			t.Fatalf("expected lookup error, got %v", err)
		}
	})

	t.Run("account id generation", func(t *testing.T) {
		swapAccountStore(t, newInMemoryAccountStore())
		swapIDGeneratorFuncs(t,
			func() (string, error) { return "", accountIDErr },
			func() (int64, error) { t.Fatal("role ID generator must not run"); return 0, nil },
		)
		_, _, err := CreateAccountWithIdentity(context.Background(), "guest", "uid-account-id")
		if !errors.Is(err, accountIDErr) {
			t.Fatalf("expected account ID error, got %v", err)
		}
	})

	t.Run("role id generation", func(t *testing.T) {
		swapAccountStore(t, newInMemoryAccountStore())
		swapIDGeneratorFuncs(t,
			func() (string, error) { return "acc-role-id", nil },
			func() (int64, error) { return 0, roleIDErr },
		)
		_, _, err := CreateAccountWithIdentity(context.Background(), "guest", "uid-role-id")
		if !errors.Is(err, roleIDErr) {
			t.Fatalf("expected role ID error, got %v", err)
		}
	})

	t.Run("store create", func(t *testing.T) {
		swapAccountStore(t, errorAccountStore{createErr: storeErr})
		swapIDGenerators(t, "acc-store", 100004)
		_, _, err := CreateAccountWithIdentity(context.Background(), "guest", "uid-store")
		if !errors.Is(err, storeErr) {
			t.Fatalf("expected store error, got %v", err)
		}
	})

	t.Run("reload after uniqueness conflict", func(t *testing.T) {
		swapAccountStore(t, &uniquenessReloadErrorStore{reloadErr: reloadErr})
		swapIDGenerators(t, "acc-reload", 100005)
		_, _, err := CreateAccountWithIdentity(context.Background(), "guest", "uid-reload")
		if !errors.Is(err, reloadErr) {
			t.Fatalf("expected reload error, got %v", err)
		}
	})
}
