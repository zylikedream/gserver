package logic

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newAccountMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	return db, mock
}

func accountRow(accountID string, roleID int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"account_id", "role_id", "created_at", "updated_at"}).
		AddRow(accountID, roleID, nil, nil)
}

func expectFindAccount(mock sqlmock.Sqlmock, platform string, platformUID string, rows *sqlmock.Rows) {
	mock.ExpectQuery(`SELECT account\.\* FROM "account" JOIN account_identity ON account_identity\.account_id = account\.account_id WHERE account_identity\.platform = \$1 AND account_identity\.platform_uid = \$2 ORDER BY "account"\."account_id" LIMIT \$3`).
		WithArgs(platform, platformUID, 1).
		WillReturnRows(rows)
}

func TestGormAccountStoreFindAccountByIdentity(t *testing.T) {
	t.Run("existing account", func(t *testing.T) {
		db, mock := newAccountMockDB(t)
		store := gormAccountStore{db: func() *gorm.DB { return db }}
		expectFindAccount(mock, "guest", "uid-1", accountRow("acc-1", 100001))

		account, err := store.FindAccountByIdentity(context.Background(), "guest", "uid-1")
		if err != nil {
			t.Fatalf("find account: %v", err)
		}
		if account == nil || account.AccountID != "acc-1" || account.RoleID != 100001 {
			t.Fatalf("unexpected account: %+v", account)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("missing account", func(t *testing.T) {
		db, mock := newAccountMockDB(t)
		store := gormAccountStore{db: func() *gorm.DB { return db }}
		expectFindAccount(mock, "guest", "missing", sqlmock.NewRows([]string{"account_id", "role_id", "created_at", "updated_at"}))

		account, err := store.FindAccountByIdentity(context.Background(), "guest", "missing")
		if err != nil {
			t.Fatalf("find missing account: %v", err)
		}
		if account != nil {
			t.Fatalf("expected nil account, got %+v", account)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("database error", func(t *testing.T) {
		db, mock := newAccountMockDB(t)
		store := gormAccountStore{db: func() *gorm.DB { return db }}
		dbErr := errors.New("database unavailable")
		mock.ExpectQuery(`SELECT account\.\* FROM "account" JOIN account_identity`).
			WithArgs("guest", "uid-2", 1).
			WillReturnError(dbErr)

		account, err := store.FindAccountByIdentity(context.Background(), "guest", "uid-2")
		if !errors.Is(err, dbErr) {
			t.Fatalf("expected database error, got account=%+v err=%v", account, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestGormAccountStoreCreateAccountWithIdentity(t *testing.T) {
	t.Run("commits both records", func(t *testing.T) {
		db, mock := newAccountMockDB(t)
		store := gormAccountStore{db: func() *gorm.DB { return db }}
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO "account"`).
			WithArgs("acc-1", int64(100001), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(`INSERT INTO "account_identity"`).
			WithArgs("guest", "uid-1", "acc-1", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		err := store.CreateAccountWithIdentity(context.Background(),
			&Account{AccountID: "acc-1", RoleID: 100001},
			&AccountIdentity{Platform: "guest", PlatformUID: "uid-1", AccountID: "acc-1"},
		)
		if err != nil {
			t.Fatalf("create account with identity: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("rolls back when identity insert fails", func(t *testing.T) {
		db, mock := newAccountMockDB(t)
		store := gormAccountStore{db: func() *gorm.DB { return db }}
		identityErr := errors.New("identity insert failed")
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO "account"`).
			WithArgs("acc-2", int64(100002), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(`INSERT INTO "account_identity"`).
			WithArgs("guest", "uid-2", "acc-2", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnError(identityErr)
		mock.ExpectRollback()

		err := store.CreateAccountWithIdentity(context.Background(),
			&Account{AccountID: "acc-2", RoleID: 100002},
			&AccountIdentity{Platform: "guest", PlatformUID: "uid-2", AccountID: "acc-2"},
		)
		if !errors.Is(err, identityErr) {
			t.Fatalf("expected identity insert error, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}
