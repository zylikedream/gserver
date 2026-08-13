package chat

// chat 消息持久化测试:go-sqlmock 断言 SQL 与参数。

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gserver/src/pkg/deps"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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
