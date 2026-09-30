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

func TestGenAutoIncIDNoDB(t *testing.T) {
	if _, err := NewGen(nil).GenAutoIncID("role"); err == nil {
		t.Fatal("GenAutoIncID = nil, want db-not-initialized error")
	}
}
