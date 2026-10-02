package uid

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// newMockDB 构造 sqlmock + gorm,供 NewGen 注入。
func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: db}))
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}

	return gdb, mock
}

// TestGenAutoIncID:契约——执行 SELECT nextval($1::regclass),参数按命名约定拼成
// uid_<group>_seq(此处 uid_role_seq),并原样返回 sequence 值 100109。
// 为什么需要:sequence 名拼错(如漏 uid_ 前缀)在真实 PG 上才会报 relation does not exist,
// 建号事务整体回滚;名字一旦与 account/logic/schema.go 里的 CREATE SEQUENCE 分叉,全服无法注册新角色。
func TestGenAutoIncID(t *testing.T) {
	gdb, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT nextval($1::regclass)`)).
		WithArgs("uid_role_seq").
		WillReturnRows(sqlmock.NewRows([]string{"nextval"}).AddRow(100109))

	id, err := NewGen(gdb).GenAutoIncID("role")
	if err != nil {
		t.Fatalf("GenAutoIncID: %v", err)
	}
	if id != 100109 {
		t.Errorf("id = %d, want 100109", id)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sql expectations: %v", err)
	}
}

// TestGenAutoIncIDError:契约——nextval 失败时把 error 向上返回,不做重试也不吞掉。
// 为什么需要:CreateAccount 靠这个 error 回滚整个事务;吞掉它会把 role_id=0 写进
// account 表,后续所有以 0 为外键的查询都指向一个不存在的角色。
func TestGenAutoIncIDError(t *testing.T) {
	gdb, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT nextval($1::regclass)`)).
		WithArgs("uid_role_seq").
		WillReturnError(errors.New("nextval failed"))

	if _, err := NewGen(gdb).GenAutoIncID("role"); err == nil {
		t.Fatal("GenAutoIncID = nil, want error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sql expectations: %v", err)
	}
}

// TestGenAutoIncIDNoDB:契约——db 为 nil 时返回 "uid gen: db not initialized",不解引用空指针。
// 为什么需要:组装根漏注入时应在发号这一步给出明确错误;空指针解引用是 panic,
// 整个进程崩掉,连日志里的错误原因都拿不到。
func TestGenAutoIncIDNoDB(t *testing.T) {
	if _, err := NewGen(nil).GenAutoIncID("role"); err == nil {
		t.Fatal("GenAutoIncID = nil, want db-not-initialized error")
	}
}
