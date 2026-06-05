package logic

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRoleMainInitRequiresAccountRecord(t *testing.T) {
	orig := lookupAccountIDByRoleID
	t.Cleanup(func() {
		lookupAccountIDByRoleID = orig
	})

	lookupAccountIDByRoleID = func(ctx context.Context, roleID int64) (string, error) {
		if roleID != 1001 {
			t.Fatalf("lookup roleID = %d, want 1001", roleID)
		}
		return "", nil
	}

	r := NewRoleMain()
	err := r.Init(context.Background(), []any{int64(1001)})
	if err == nil {
		t.Fatal("expected init error when account record is missing")
	}
	if !strings.Contains(err.Error(), "role account not exist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRoleMainInitAcceptsExistingAccountRecord(t *testing.T) {
	orig := lookupAccountIDByRoleID
	t.Cleanup(func() {
		lookupAccountIDByRoleID = orig
	})

	lookupAccountIDByRoleID = func(context.Context, int64) (string, error) {
		return "acc_123", nil
	}

	r := NewRoleMain()
	if err := r.Init(context.Background(), []any{int64(1001)}); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
}

func TestRoleMainInitPropagatesAccountLookupError(t *testing.T) {
	orig := lookupAccountIDByRoleID
	t.Cleanup(func() {
		lookupAccountIDByRoleID = orig
	})

	wantErr := errors.New("db down")
	lookupAccountIDByRoleID = func(context.Context, int64) (string, error) {
		return "", wantErr
	}

	r := NewRoleMain()
	err := r.Init(context.Background(), []any{int64(1001)})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Init error = %v, want %v", err, wantErr)
	}
}
