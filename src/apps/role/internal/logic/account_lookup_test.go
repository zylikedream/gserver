package logic

import (
	"context"
	"errors"
	"testing"

	accountlogic "gserver/src/apps/account/logic"
)

func TestDefaultLookupAccountIDByRoleIDReturnsAccountID(t *testing.T) {
	orig := loadAccountByRoleID
	t.Cleanup(func() {
		loadAccountByRoleID = orig
	})

	loadAccountByRoleID = func(ctx context.Context, roleID int64) (*accountlogic.Account, error) {
		if roleID != 2001 {
			t.Fatalf("roleID = %d, want 2001", roleID)
		}
		return &accountlogic.Account{AccountID: "acc_2001", RoleID: 2001}, nil
	}

	accountID, err := defaultLookupAccountIDByRoleID(context.Background(), 2001)
	if err != nil {
		t.Fatalf("default lookup returned error: %v", err)
	}
	if accountID != "acc_2001" {
		t.Fatalf("accountID = %s, want acc_2001", accountID)
	}
}

func TestDefaultLookupAccountIDByRoleIDReturnsEmptyWhenMissing(t *testing.T) {
	orig := loadAccountByRoleID
	t.Cleanup(func() {
		loadAccountByRoleID = orig
	})

	loadAccountByRoleID = func(context.Context, int64) (*accountlogic.Account, error) {
		return nil, nil
	}

	accountID, err := defaultLookupAccountIDByRoleID(context.Background(), 2002)
	if err != nil {
		t.Fatalf("default lookup returned error: %v", err)
	}
	if accountID != "" {
		t.Fatalf("accountID = %s, want empty", accountID)
	}
}

func TestDefaultLookupAccountIDByRoleIDPropagatesError(t *testing.T) {
	orig := loadAccountByRoleID
	t.Cleanup(func() {
		loadAccountByRoleID = orig
	})

	wantErr := errors.New("db down")
	loadAccountByRoleID = func(context.Context, int64) (*accountlogic.Account, error) {
		return nil, wantErr
	}

	_, err := defaultLookupAccountIDByRoleID(context.Background(), 2003)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}
