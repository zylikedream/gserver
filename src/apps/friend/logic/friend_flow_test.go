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

// TestAcceptRequest_Success:同意申请必须在同一事务里完成四件事——按序 FOR UPDATE 锁双方
// 行、saveRow 写回 me/other(各自清 incoming/outgoing)、INSERT friend_relation 两条对称关系。
// 为什么需要:relation 表是一行两列的对称约定,少写一条就会出现"我加了他他没加我";
// 少一次 saveRow 则申请仍留在 incoming,重复点击会再加一次好友。
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

// TestAcceptRequest_NoApply:me.Incoming 不含 fromID 时必须返回可被 errors.Is 命中的
// ErrApplyNotFound,并且只回滚、不发任何 UPDATE/INSERT。
// 为什么需要:申请可能已被另一端处理或过期;这里若继续写,会凭空造出一条从未申请过的
// 好友关系(资历伪造),若写成别的错误码,上层无法提示"申请不存在"。
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

// TestAcceptRequest_FriendFull:我方 Friends 长度 >= cfg.FriendMaxCount(测试用 2)时
// 必须返回哨兵 ErrFriendFull 并回滚。
// 为什么需要:handler 靠 errors.Is 把这个哨兵翻译成"好友数量已达上限"提示;
// 上限一旦失效,好友数会无限膨胀,好友列表与关系表体积失控且无法解释。
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

// TestAcceptRequest_OtherFriendFull:对方 Friends 已满时同样必须失败回滚(非哨兵错误)。
// 为什么需要:只判我方上限会让"对方已满"被写成半边关系——我方有他、他没有我,
// 好友关系不对称且双方看到的列表不一致,这种脏数据只能人工修库。
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

// TestRejectRequest_Success:拒绝只清 incoming/outgoing 并 saveRow 两次,不写 friend_relation。
// 为什么需要:拒绝路径若误加关系行,会出现"被拒绝的人却出现在好友列表"的幽灵好友,
// 且 list 按 PlayerID 去不掉,永久残留。
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

// TestRejectRequest_NoApply:申请不存在时返回 ErrApplyNotFound 且回滚,不做任何写入。
// 为什么需要:重复拒绝/延迟到达的拒绝请求会走到这里,写入就会凭空删掉另一条正常申请。
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

// TestRemoveFriend_Success:删好友必须在同一事务里清双方 Friends、saveRow 两次
// (含冷却列)、DELETE friend_relation 的两条对称行。
// 为什么需要:relation 表残留会让列表查询仍把两人视为好友;冷却不写则玩家可以立刻
// 反复加删同一好友刷消息,是防骚扰的唯一闸门。
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

// TestLockBoth_Ascending:入参已升序(100,200)时,lockBoth 按序加锁 100 → 200。
// 这是基准情形;真正的契约在 _Descending——两个测试断言的是**同一条**不变量。
// 为什么需要:sqlmock 的 ExpectQuery 是有序匹配,反序加锁会直接参数不匹配而失败。
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

// TestLockBoth_Descending:入参降序(200,100)时,lockBoth 仍必须先锁较小者 100。
// 为什么需要:加好友与删好友是双向操作,若按各自调用方的参数顺序加锁,两个反向事务
// 会各持对方需要的行锁,互相等待——死锁,且只在并发下偶发,单线程测试永远测不到。
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
