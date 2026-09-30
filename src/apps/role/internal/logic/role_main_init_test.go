package logic

import (
	"errors"
	"strings"
	"testing"

	"gserver/core/gxyactor/gxyactortest"
	"gserver/src/lib"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
)

// roleCtorWithDB 注入 sqlmock 连接:初始化段要查 account 表(它决定该 role 是否可激活),
// 测试不再打桩该查询,而是断言真实 SQL。
func roleCtorWithDB(db *gorm.DB) func() *RoleMain {
	return func() *RoleMain {
		r := NewRoleMain()
		r.deps.DB = db
		return r
	}
}

// spawnRole 在 mock 节点上创建真实 role actor,并用内存实现替换所有权,
// 使其不依赖 Redis。返回的 actor 已通过 Init。
func spawnRole(t *testing.T, db *gorm.DB, roleID int64) *RoleMain {
	t.Helper()
	initAllTestConfig(t)
	gxyactortest.StubOwnership(t)
	r, _ := gxyactortest.Spawn(t, lib.ROLE_ACTOR_TYPE, roleCtorWithDB(db), roleID)
	return r
}

// 账号记录缺失时不得激活:这类 role 无法提供任何业务能力。
func TestRoleMainInitRequiresAccountRecord(t *testing.T) {
	db, mock := newGormMock(t)
	mock.ExpectQuery(selectAccountByRoleID).WithArgs(int64(1001), 1).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "role_id"}))

	gxyactortest.StubOwnership(t)
	_, err := gxyactortest.SpawnErr(t, lib.ROLE_ACTOR_TYPE, roleCtorWithDB(db), int64(1001))
	if err == nil {
		t.Fatal("expected init error when account record is missing")
	}
	if !strings.Contains(err.Error(), "role account not exist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 账号存在时 Init 成功,且同步段已取得所有权。
func TestRoleMainInitAcceptsExistingAccountRecord(t *testing.T) {
	db, mock := newGormMock(t)
	mock.ExpectQuery(selectAccountByRoleID).WithArgs(int64(1001), 1).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "role_id"}).AddRow("acc_123", 1001))

	r := spawnRole(t, db, 1001)
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
	db, mock := newGormMock(t)
	wantErr := errors.New("db down")
	mock.ExpectQuery(selectAccountByRoleID).WithArgs(int64(1001), 1).WillReturnError(wantErr)

	gxyactortest.StubOwnership(t)
	_, err := gxyactortest.SpawnErr(t, lib.ROLE_ACTOR_TYPE, roleCtorWithDB(db), int64(1001))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Init error = %v, want %v", err, wantErr)
	}
}
