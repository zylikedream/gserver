package logic

// friend 核心流程事务测试(go-sqlmock):同意/拒绝申请、删除好友、双向锁。
// 复用 sendrequest_test.go 的 newFriendDBMock/testFriendConfig。

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
)

// expectLockRow 期望一次 lockRow 命中已有行(FOR UPDATE 返回行)。
func expectLockRow(mock sqlmock.Sqlmock, playerID int64, friends, incoming, outgoing, cooldowns string) {
	mock.ExpectQuery(`SELECT \* FROM "friend_data" WHERE "friend_data"."player_id" = \$1 ORDER BY "friend_data"."player_id" LIMIT \$2 FOR UPDATE`).
		WithArgs(playerID, 1).
		WillReturnRows(sqlmock.NewRows(friendDataColumns()).
			AddRow(playerID, friends, incoming, outgoing, cooldowns, nil))
}

// expectSaveRow 期望一次 gorm Save 全列 UPDATE。
func expectSaveRow(mock sqlmock.Sqlmock, playerID int64) {
	mock.ExpectExec(`UPDATE "friend_data" SET .* WHERE "player_id" = \$6`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), playerID).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

// ========== AcceptRequest ==========

func TestAcceptRequest_Success(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)
	ctx := context.Background()

	mock.ExpectBegin()
	expectLockRow(mock, 100, "[]", `[{"player_id":200,"apply_at":1}]`, "[]", "[]")
	expectLockRow(mock, 200, "[]", "[]", `[{"player_id":100,"apply_at":1}]`, "[]")
	expectSaveRow(mock, 100)
	expectSaveRow(mock, 200)
	mock.ExpectExec(`INSERT INTO "friend_relation"`).
		WithArgs(int64(100), int64(200), sqlmock.AnyArg(), int64(200), int64(100), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 2))
	mock.ExpectCommit()

	if err := AcceptRequest(ctx, 100, 200, testFriendConfig(), gormDB); err != nil {
		t.Fatalf("AcceptRequest: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAcceptRequest_NoApply(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)
	ctx := context.Background()

	mock.ExpectBegin()
	expectLockRow(mock, 100, "[]", "[]", "[]", "[]") // incoming 无 200
	expectLockRow(mock, 200, "[]", "[]", "[]", "[]")
	mock.ExpectRollback()

	if err := AcceptRequest(ctx, 100, 200, testFriendConfig(), gormDB); !errors.Is(err, ErrApplyNotFound) {
		t.Fatalf("expected ErrApplyNotFound, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAcceptRequest_FriendFull(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)
	ctx := context.Background()

	full := `[{"player_id":1,"added_at":1},{"player_id":2,"added_at":1}]`
	mock.ExpectBegin()
	expectLockRow(mock, 100, full, `[{"player_id":200,"apply_at":1}]`, "[]", "[]")
	expectLockRow(mock, 200, "[]", "[]", "[]", "[]")
	mock.ExpectRollback()

	cfg := &Config{ApplySendLimit: 10, ApplyReceiveLimit: 10, FriendMaxCount: 2}
	if err := AcceptRequest(ctx, 100, 200, cfg, gormDB); !errors.Is(err, ErrFriendFull) {
		t.Fatalf("expected ErrFriendFull, got %v", err)
	}
}

func TestAcceptRequest_OtherFriendFull(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)
	ctx := context.Background()

	full := `[{"player_id":1,"added_at":1},{"player_id":2,"added_at":1}]`
	mock.ExpectBegin()
	expectLockRow(mock, 100, "[]", `[{"player_id":200,"apply_at":1}]`, "[]", "[]")
	expectLockRow(mock, 200, full, "[]", "[]", "[]")
	mock.ExpectRollback()

	cfg := &Config{ApplySendLimit: 10, ApplyReceiveLimit: 10, FriendMaxCount: 2}
	if err := AcceptRequest(ctx, 100, 200, cfg, gormDB); err == nil {
		t.Fatal("expected error for other side full")
	}
}

// ========== RejectRequest ==========

func TestRejectRequest_Success(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)
	ctx := context.Background()

	mock.ExpectBegin()
	expectLockRow(mock, 100, "[]", `[{"player_id":200,"apply_at":1}]`, "[]", "[]")
	expectLockRow(mock, 200, "[]", "[]", `[{"player_id":100,"apply_at":1}]`, "[]")
	expectSaveRow(mock, 100)
	expectSaveRow(mock, 200)
	mock.ExpectCommit()

	if err := RejectRequest(ctx, 100, 200, gormDB); err != nil {
		t.Fatalf("RejectRequest: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRejectRequest_NoApply(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)
	ctx := context.Background()

	mock.ExpectBegin()
	expectLockRow(mock, 100, "[]", "[]", "[]", "[]")
	expectLockRow(mock, 200, "[]", "[]", "[]", "[]")
	mock.ExpectRollback()

	if err := RejectRequest(ctx, 100, 200, gormDB); !errors.Is(err, ErrApplyNotFound) {
		t.Fatalf("expected ErrApplyNotFound, got %v", err)
	}
}

// ========== RemoveFriend ==========

func TestRemoveFriend_Success(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)
	ctx := context.Background()

	mock.ExpectBegin()
	expectLockRow(mock, 100, `[{"player_id":200,"added_at":1}]`, "[]", "[]", "[]")
	expectLockRow(mock, 200, `[{"player_id":100,"added_at":1}]`, "[]", "[]", "[]")
	expectSaveRow(mock, 100)
	expectSaveRow(mock, 200)
	mock.ExpectExec(`DELETE FROM "friend_relation"`).
		WithArgs(int64(100), int64(200), int64(200), int64(100)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	if err := RemoveFriend(ctx, 100, 200, testFriendConfig(), gormDB); err != nil {
		t.Fatalf("RemoveFriend: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

// ========== lockBoth 顺序 ==========

func TestLockBoth_Ascending(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)
	mock.ExpectBegin()
	tx := gormDB.Begin()

	// a(100) < b(200): 先锁 100 再锁 200
	mock.ExpectQuery(`SELECT \* FROM "friend_data" WHERE "friend_data"."player_id" = \$1 ORDER BY "friend_data"."player_id" LIMIT \$2 FOR UPDATE`).
		WithArgs(int64(100), 1).
		WillReturnRows(sqlmock.NewRows(friendDataColumns()).AddRow(100, "[]", "[]", "[]", "[]", nil))
	mock.ExpectQuery(`SELECT \* FROM "friend_data" WHERE "friend_data"."player_id" = \$1 ORDER BY "friend_data"."player_id" LIMIT \$2 FOR UPDATE`).
		WithArgs(int64(200), 1).
		WillReturnRows(sqlmock.NewRows(friendDataColumns()).AddRow(200, "[]", "[]", "[]", "[]", nil))

	first, second, err := lockBoth(tx, 100, 200)
	if err != nil {
		t.Fatalf("lockBoth: %v", err)
	}
	if first.PlayerID != 100 || second.PlayerID != 200 {
		t.Fatalf("expected order (100,200), got (%d,%d)", first.PlayerID, second.PlayerID)
	}
}

func TestLockBoth_Descending(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)
	mock.ExpectBegin()
	tx := gormDB.Begin()

	// a(200) > b(100): 仍先锁 100(小者)再锁 200
	mock.ExpectQuery(`SELECT \* FROM "friend_data" WHERE "friend_data"."player_id" = \$1 ORDER BY "friend_data"."player_id" LIMIT \$2 FOR UPDATE`).
		WithArgs(int64(100), 1).
		WillReturnRows(sqlmock.NewRows(friendDataColumns()).AddRow(100, "[]", "[]", "[]", "[]", nil))
	mock.ExpectQuery(`SELECT \* FROM "friend_data" WHERE "friend_data"."player_id" = \$1 ORDER BY "friend_data"."player_id" LIMIT \$2 FOR UPDATE`).
		WithArgs(int64(200), 1).
		WillReturnRows(sqlmock.NewRows(friendDataColumns()).AddRow(200, "[]", "[]", "[]", "[]", nil))

	first, second, err := lockBoth(tx, 200, 100)
	if err != nil {
		t.Fatalf("lockBoth: %v", err)
	}
	if first.PlayerID != 100 || second.PlayerID != 200 {
		t.Fatalf("expected normalized order (100,200), got (%d,%d)", first.PlayerID, second.PlayerID)
	}
}

var _ = gorm.ErrRecordNotFound // 引用 gorm 避免误删 import
