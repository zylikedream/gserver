package logic

// friend SendRequest 事务测试:go-sqlmock 验证行锁、双行更新与提交。

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newFriendDBMock(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return gormDB, mock
}

func testFriendConfig() *Config {
	return &Config{ApplySendLimit: 10, ApplyReceiveLimit: 10, FriendMaxCount: 100}
}

func friendDataColumns() []string {
	return []string{"player_id", "friends", "incoming", "outgoing", "cooldowns", "update_at"}
}

// TestSendRequest_Success:发起申请要按升序 FOR UPDATE 锁双方两行(行不存在时 lockRow
// 内 Create 空行)、各 saveRow 一次(Save 为全列 UPDATE)、并以 Commit 收尾。
// 为什么需要:outgoing 与 incoming 必须落在同一次提交里;漏一次 saveRow 会出现
// "我发出去了他收不到",申请永远无人可见,而 sqlmock 的有序期望正好能抓住次序错乱。
func TestSendRequest_Success(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)
	ctx := context.Background()

	mock.ExpectBegin()
	// lockBoth: 100 < 200, 按序锁行; 行不存在 → lockRow 内 Create 空行
	mock.ExpectQuery(`SELECT \* FROM "friend_data" WHERE "friend_data"."player_id" = \$1 ORDER BY "friend_data"."player_id" LIMIT \$2 FOR UPDATE`).
		WithArgs(int64(100), 1).
		WillReturnRows(sqlmock.NewRows(friendDataColumns()))
	mock.ExpectQuery(`INSERT INTO "friend_data" .* RETURNING .*`).
		WithArgs(sqlmock.AnyArg(), int64(100)).
		WillReturnRows(sqlmock.NewRows(friendDataColumns()).AddRow(100, "[]", "[]", "[]", "[]", nil))
	mock.ExpectQuery(`SELECT \* FROM "friend_data" WHERE "friend_data"."player_id" = \$1 ORDER BY "friend_data"."player_id" LIMIT \$2 FOR UPDATE`).
		WithArgs(int64(200), 1).
		WillReturnRows(sqlmock.NewRows(friendDataColumns()))
	mock.ExpectQuery(`INSERT INTO "friend_data" \("update_at","player_id"\) VALUES \(\$1,\$2\) RETURNING .*`).
		WithArgs(sqlmock.AnyArg(), int64(200)).
		WillReturnRows(sqlmock.NewRows(friendDataColumns()).AddRow(200, "[]", "[]", "[]", "[]", nil))
	// saveRow × 2 (gorm Save 全列更新)
	mock.ExpectExec(`UPDATE "friend_data" SET .* WHERE "player_id" = \$6`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), int64(100)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "friend_data" SET .* WHERE "player_id" = \$6`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), int64(200)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := SendRequest(ctx, 100, 200, testFriendConfig(), gormDB); err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

// TestSendRequest_SelfAdd:fromID == toID 必须在开启事务之前返回 ErrSelfAdd,
// 且不产生任何 SQL。
// 为什么需要:自加会把同一 player_id 同时写进 outgoing 和 incoming,制造自己向自己
// 发申请的死循环,后续所有同意/拒绝都被这条脏记录卡住。
func TestSendRequest_SelfAdd(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)

	err := SendRequest(context.Background(), 100, 100, testFriendConfig(), gormDB)
	if !errors.Is(err, ErrSelfAdd) {
		t.Fatalf("expected ErrSelfAdd, got %v", err)
	}
	// 无任何 SQL(事务未开启)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("self-add should not touch db: %v", err)
	}
}

// TestSendRequest_AlreadyFriend:双方 friends 已互相包含时返回 ErrAlreadyFriend 并回滚,
// 不追加任何申请。
// 为什么需要:漏这条判定会重复堆积申请行并触发"发送申请数量已达上限",
// 让玩家再也加不了任何新好友。
func TestSendRequest_AlreadyFriend(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)
	ctx := context.Background()

	// 双方已是好友: 行数据中 friends 含对方
	meRow := sqlmock.NewRows(friendDataColumns()).
		AddRow(100, `[{"player_id":200,"friend_at":1000}]`, `[]`, `[]`, `[]`, nil)
	targetRow := sqlmock.NewRows(friendDataColumns()).
		AddRow(200, `[{"player_id":100,"friend_at":1000}]`, `[]`, `[]`, `[]`, nil)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "friend_data" WHERE "friend_data"."player_id" = \$1 ORDER BY "friend_data"."player_id" LIMIT \$2 FOR UPDATE`).
		WithArgs(int64(100), 1).
		WillReturnRows(meRow)
	mock.ExpectQuery(`SELECT \* FROM "friend_data" WHERE "friend_data"."player_id" = \$1 ORDER BY "friend_data"."player_id" LIMIT \$2 FOR UPDATE`).
		WithArgs(int64(200), 1).
		WillReturnRows(targetRow)
	mock.ExpectRollback()

	err := SendRequest(ctx, 100, 200, testFriendConfig(), gormDB)
	if !errors.Is(err, ErrAlreadyFriend) {
		t.Fatalf("expected ErrAlreadyFriend, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
