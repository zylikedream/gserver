package chat

// chat 消息持久化测试:go-sqlmock 断言 SQL 与参数。

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gserver/src/pkg/deps"
)

func newChatDBMock(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
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

// TestStorePrivateMsg_SQL:写入必须先把两端角色按 sortIDs 归一化成
// (min_role_id,max_role_id),列顺序与取值固定。这里刻意钉住 SQL 文本:列序错位不是
// 编译期可见的,只会在真实库里把 sender_id 写进 min_role_id。为什么需要:归一化一旦
// 丢失,A 查 B 的私聊就查不到自己刚发的消息,聊天记录对玩家凭空消失且无报错。
func TestStorePrivateMsg_SQL(t *testing.T) {
	gormDB, mock := newChatDBMock(t)
	d := deps.Deps{DB: gormDB}

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "chat_private_message" \("min_role_id","max_role_id","sender_id","content","created_at"\) VALUES \(\$1,\$2,\$3,\$4,\$5\) RETURNING "id"`).
		WithArgs(int64(1), int64(2), int64(2), "hi", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	ts, err := StorePrivateMsg(context.Background(), d, 2, 1, "hi")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if ts <= 0 {
		t.Fatalf("expected timestamp, got %d", ts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

// TestGetPrivateHistory_SQL:查询必须带 ORDER BY created_at DESC + LIMIT,
// 且拿到的是 DB 的反序结果——GetPrivateHistory 用 slices.Backward 翻回正序再交给调用方。
// 为什么需要:DESC 一旦被丢掉,回放顺序静默反转,聊天记录从最旧到最新显示;
// 少一个 LIMIT 则拉全表,单会话历史无限增长。
func TestGetPrivateHistory_SQL(t *testing.T) {
	gormDB, mock := newChatDBMock(t)
	d := deps.Deps{DB: gormDB}

	rows := sqlmock.NewRows([]string{"id", "min_role_id", "max_role_id", "sender_id", "content", "created_at"}).
		AddRow(3, 1, 2, 2, "hi", time.Unix(1000, 0)).
		AddRow(2, 1, 2, 1, "hello", time.Unix(999, 0))
	mock.ExpectQuery(`SELECT \* FROM "chat_private_message" WHERE min_role_id = \$1 AND max_role_id = \$2 ORDER BY created_at DESC LIMIT \$3`).
		WithArgs(int64(1), int64(2), 50).
		WillReturnRows(rows)

	msgs, err := GetPrivateHistory(context.Background(), d, 1, 2, 50)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 msgs, got %d", len(msgs))
	}
	// ORDER BY created_at DESC → 反序返回(旧的在前)
	if msgs[0].Content != "hello" || msgs[1].Content != "hi" {
		t.Fatalf("unexpected order: %+v", msgs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

// TestStoreSystemMsg_SQL:系统公告 INSERT 的列集合/顺序固定,返回 CreatedAt.Unix() 作为时间戳。
// 为什么需要:和私聊同理钉住列序——content 与 created_at 写反不会报错,只表现为公告
// 内容变成时间戳、时间戳变成公告文本;返回 0 还会让上层按 0 秒排时间线。
func TestStoreSystemMsg_SQL(t *testing.T) {
	gormDB, mock := newChatDBMock(t)
	d := deps.Deps{DB: gormDB}

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "chat_system_message" \("content","created_at"\) VALUES \(\$1,\$2\) RETURNING "id"`).
		WithArgs("announce", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	ts, err := StoreSystemMsg(context.Background(), d, "announce")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if ts <= 0 {
		t.Fatalf("expected timestamp, got %d", ts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
