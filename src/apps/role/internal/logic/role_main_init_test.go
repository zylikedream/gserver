package logic

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gserver/core/gxyactor/gxyactortest"
)

// spawnRole 在 mock 节点上创建真实 role actor,并用内存实现替换所有权,
// 使其不依赖 Redis。返回的 actor 已通过 Init。
func spawnRole(t *testing.T, roleID int64) *RoleMain {
	t.Helper()
	gxyactortest.StubOwnership(t)
	r, _ := gxyactortest.Spawn(t, NewRoleMain, roleID)
	return r
}

// 账号记录缺失时不得激活:这类 role 无法提供任何业务能力。
func TestRoleMainInitRequiresAccountRecord(t *testing.T) {
	orig := lookupAccountIDByRoleID
	t.Cleanup(func() { lookupAccountIDByRoleID = orig })
	lookupAccountIDByRoleID = func(_ context.Context, roleID int64) (string, error) {
		if roleID != 1001 {
			t.Fatalf("lookup roleID = %d, want 1001", roleID)
		}
		return "", nil
	}

	gxyactortest.StubOwnership(t)
	_, err := gxyactortest.SpawnErr(t, NewRoleMain, int64(1001))
	if err == nil {
		t.Fatal("expected init error when account record is missing")
	}
	if !strings.Contains(err.Error(), "role account not exist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 账号存在时 Init 成功,且同步段已取得所有权。
func TestRoleMainInitAcceptsExistingAccountRecord(t *testing.T) {
	orig := lookupAccountIDByRoleID
	t.Cleanup(func() { lookupAccountIDByRoleID = orig })
	lookupAccountIDByRoleID = func(context.Context, int64) (string, error) {
		return "acc_123", nil
	}

	r := spawnRole(t, 1001)
	if r.RoleID != 1001 {
		t.Fatalf("RoleID = %d, want 1001", r.RoleID)
	}
	// 所有权必须在同步初始化段取得(见 invariants #3)。
	own := gxyactortest.LastOwnership()
	if len(own.Claimed()) != 1 {
		t.Fatalf("claimed = %v, want exactly one claim", own.Claimed())
	}
}

// 账号查询失败必须原样上抛,不得被吞掉。
func TestRoleMainInitPropagatesAccountLookupError(t *testing.T) {
	orig := lookupAccountIDByRoleID
	t.Cleanup(func() { lookupAccountIDByRoleID = orig })

	wantErr := errors.New("db down")
	lookupAccountIDByRoleID = func(context.Context, int64) (string, error) {
		return "", wantErr
	}

	gxyactortest.StubOwnership(t)
	_, err := gxyactortest.SpawnErr(t, NewRoleMain, int64(1001))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Init error = %v, want %v", err, wantErr)
	}
}
