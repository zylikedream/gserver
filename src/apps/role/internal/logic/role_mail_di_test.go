package logic

// 依赖注入测试样板:go-sqlmock 断言 SQL 语句与参数,替换 notifyMailUpdate
// 为 no-op(可替换函数变量,非 gomonkey 打桩),专注 DB 行为验证。

import (
	"context"
	"testing"
	"time"

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/src/pkg/deps"
	"gserver/src/pkg/gameconfig"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// newGormMock 创建 go-sqlmock 驱动的 *gorm.DB。
func newGormMock(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
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

// newMailTestDeps 构造注入 mock 依赖的 RoleMain,配表来自真实 gameconfig/json。
func newMailTestDeps(t *testing.T, db *gorm.DB) (*RoleMain, *gamecfg.GardenMailConfig) {
	t.Helper()
	rows := loadTestTable(t, "garden_tbmailconfig")
	tbMailConfig, err := gamecfg.NewGardenTbMailConfig(rows)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &gameconfig.GameConfig{Tables: &gamecfg.Tables{TbMailConfig: tbMailConfig}}
	main := &RoleMain{deps: deps.Deps{DB: db, Cfg: cfg}}
	return main, tbMailConfig.Get()
}

func TestRefreshMailCache_SQLMock(t *testing.T) {
	gormDB, mock := newGormMock(t)
	main, mailCfg := newMailTestDeps(t, gormDB)
	mail := &RoleMail{RoleModule: RoleModule{Role: main}}
	main.Mail = mail
	mail.RoleID = 1001

	now := time.Now().Unix()
	// 个人邮件查询
	mock.ExpectQuery(`SELECT \* FROM "personal_mail" WHERE role_id = \$1 ORDER BY id DESC LIMIT \$2`).
		WithArgs(1001, int(mailCfg.MailMaxCount)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "role_id", "title", "content", "attachments", "send_at", "expire_at"}).
			AddRow(1, 1001, "personal-1", "body", nil, now-10, 0))
	// 系统邮件查询(初始 LastSysMailID=0)
	mock.ExpectQuery(`SELECT \* FROM "sys_mail" WHERE id > \$1 AND \(expire_at = 0 OR expire_at >= \$2\) ORDER BY id ASC LIMIT \$3`).
		WithArgs(0, now, int(mailCfg.MailMaxCount)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "content", "attachments", "create_at", "expire_at"}).
			AddRow(12, "system-1", "body", nil, now, 0))
	// 系统邮件状态已填充 → 按 ID 回查可见系统邮件
	mock.ExpectQuery(`SELECT \* FROM "sys_mail" WHERE id IN \(\$1\) AND \(expire_at = 0 OR expire_at >= \$2\)`).
		WithArgs(12, now).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "content", "attachments", "create_at", "expire_at"}).
			AddRow(12, "system-1", "body", nil, now, 0))

	if err := mail.RefreshMailCache(context.Background()); err != nil {
		t.Fatalf("RefreshMailCache error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations not met: %v", err)
	}

	if len(mail.mailCache) != 2 {
		t.Fatalf("expected 2 mails in cache, got %d", len(mail.mailCache))
	}
	// 系统邮件 SendAt 更新,排序靠前(同 SendAt 时 ID 大在前)
	if mail.mailCache[0].ID != 12 {
		t.Fatalf("expected system mail first, got %+v", mail.mailCache[0])
	}
	if mail.mailCache[1].ID != 1 {
		t.Fatalf("expected personal mail second, got %+v", mail.mailCache[1])
	}
	// 状态已填充
	if st, ok := mail.state.States["12"]; !ok || !st.IsSysMail {
		t.Fatalf("expected sys mail state, got %+v", mail.state.States)
	}
}

func TestSendMail_SQLMock(t *testing.T) {
	gormDB, mock := newGormMock(t)
	main, _ := newMailTestDeps(t, gormDB)

	// 通知走 no-op,聚焦 DB 行为
	origNotify := notifyMailUpdate
	notifyMailUpdate = func(ctx context.Context, roleID, mailID int64) {}
	t.Cleanup(func() { notifyMailUpdate = origNotify })

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "personal_mail" \("role_id","title","content","attachments","send_at","expire_at"\) VALUES \(\$1,\$2,\$3,\$4,\$5,\$6\) RETURNING "id"`).
		WithArgs(int64(2001), "title", "content", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(99))
	mock.ExpectCommit()

	err := SendMail(context.Background(), main.deps, 2001, SendMailOpts{Title: "title", Content: "content"})
	if err != nil {
		t.Fatalf("SendMail error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations not met: %v", err)
	}
}

func TestSendMail_ValidationError(t *testing.T) {
	gormDB, mock := newGormMock(t)
	main, mailCfg := newMailTestDeps(t, gormDB)
	if mailCfg.TitleLimit <= 0 {
		t.Skip("配表未配置 TitleLimit,跳过超限断言")
	}

	longTitle := make([]rune, int(mailCfg.TitleLimit)+1)
	for i := range longTitle {
		longTitle[i] = 'a'
	}
	err := SendMail(context.Background(), main.deps, 2001, SendMailOpts{Title: string(longTitle), Content: "content"})
	if err == nil {
		t.Fatal("expected title too long error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations not met: %v", err)
	}
}
