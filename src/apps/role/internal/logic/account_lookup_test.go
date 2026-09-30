package logic

import (
	"context"
	"errors"
	"testing"

	"gserver/src/pkg/deps"

	"github.com/DATA-DOG/go-sqlmock"
)

// loadAccountByRoleID / lookupAccountIDByRoleID 的 SQL 断言(依赖经 deps 注入,不再打桩)。

const selectAccountByRoleID = `SELECT \* FROM "account" WHERE role_id = \$1 ORDER BY "account"\."account_id" LIMIT \$2`

func newAccountLookupRole(t *testing.T) (*RoleMain, sqlmock.Sqlmock) {
	t.Helper()
	db, mock := newGormMock(t)
	initAllTestConfig(t)
	return &RoleMain{RoleID: 2001, deps: deps.Deps{DB: db}}, mock
}

func TestLoadAccountByRoleID(t *testing.T) {
	t.Run("命中返回账号", func(t *testing.T) {
		main, mock := newAccountLookupRole(t)
		mock.ExpectQuery(selectAccountByRoleID).WithArgs(int64(2001), 1).
			WillReturnRows(sqlmock.NewRows([]string{"account_id", "role_id"}).AddRow("acc_2001", 2001))

		account, err := main.loadAccountByRoleID(context.Background())
		if err != nil {
			t.Fatalf("loadAccountByRoleID: %v", err)
		}
		if account == nil || account.AccountID != "acc_2001" {
			t.Fatalf("account = %+v, want acc_2001", account)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("sql expectations not met: %v", err)
		}
	})

	t.Run("无记录返回 nil", func(t *testing.T) {
		main, mock := newAccountLookupRole(t)
		mock.ExpectQuery(selectAccountByRoleID).WithArgs(int64(2001), 1).
			WillReturnRows(sqlmock.NewRows([]string{"account_id", "role_id"}))

		account, err := main.loadAccountByRoleID(context.Background())
		if err != nil {
			t.Fatalf("loadAccountByRoleID: %v", err)
		}
		if account != nil {
			t.Fatalf("account = %+v, want nil", account)
		}
	})

	t.Run("查询失败上抛", func(t *testing.T) {
		main, mock := newAccountLookupRole(t)
		wantErr := errors.New("db down")
		mock.ExpectQuery(selectAccountByRoleID).WithArgs(int64(2001), 1).WillReturnError(wantErr)

		if _, err := main.loadAccountByRoleID(context.Background()); !errors.Is(err, wantErr) {
			t.Fatalf("error = %v, want %v", err, wantErr)
		}
	})
}

func TestLookupAccountIDByRoleID(t *testing.T) {
	t.Run("命中返回 account_id", func(t *testing.T) {
		main, mock := newAccountLookupRole(t)
		mock.ExpectQuery(selectAccountByRoleID).WithArgs(int64(2001), 1).
			WillReturnRows(sqlmock.NewRows([]string{"account_id", "role_id"}).AddRow("acc_2001", 2001))

		accountID, err := main.lookupAccountIDByRoleID(context.Background())
		if err != nil {
			t.Fatalf("lookupAccountIDByRoleID: %v", err)
		}
		if accountID != "acc_2001" {
			t.Fatalf("accountID = %q, want acc_2001", accountID)
		}
	})

	t.Run("无记录返回空", func(t *testing.T) {
		main, mock := newAccountLookupRole(t)
		mock.ExpectQuery(selectAccountByRoleID).WithArgs(int64(2001), 1).
			WillReturnRows(sqlmock.NewRows([]string{"account_id", "role_id"}))

		accountID, err := main.lookupAccountIDByRoleID(context.Background())
		if err != nil {
			t.Fatalf("lookupAccountIDByRoleID: %v", err)
		}
		if accountID != "" {
			t.Fatalf("accountID = %q, want empty", accountID)
		}
	})
}
