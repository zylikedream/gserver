package logic

// guild actor 持久化测试:go-sqlmock 注入 g.db,验证 DelayInit 加载与 save 保存。

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newGuildDBMock(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
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

func guildRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "level", "icon", "declaration", "announcement",
		"need_approval", "member_count", "leader_id", "members", "apply_list", "logs", "created_at", "updated_at", "version"}).
		AddRow(1, "TestGuild", 2, "icon1", "decl", "anno", true, 3, 100,
			`[{"role_id":100,"position":1,"joined_at":1000}]`, `[]`, `[]`,
			time.Unix(1000, 0), time.Unix(1000, 0), 1)
}

func TestGuildActor_LoadFromDB(t *testing.T) {
	gormDB, mock := newGuildDBMock(t)
	g := &GuildActor{GuildID: 1, db: gormDB}

	mock.ExpectQuery(`SELECT \* FROM "guild" WHERE "guild"."id" = \$1 ORDER BY "guild"."id" LIMIT \$2`).
		WithArgs(int64(1), 1).
		WillReturnRows(guildRows())

	if err := g.loadFromDB(context.Background()); err != nil {
		t.Fatalf("loadFromDB: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
	if g.Data == nil || g.Data.ID != 1 || g.Data.Name != "TestGuild" {
		t.Fatalf("unexpected guild data: %+v", g.Data)
	}
	if g.Data.Level != 2 || g.Data.LeaderID != 100 {
		t.Fatalf("unexpected guild fields: %+v", g.Data)
	}
	if len(g.Data.Members) != 1 || g.Data.Members[0].RoleID != 100 {
		t.Fatalf("unexpected members: %+v", g.Data.Members)
	}
}

func TestGuildActor_LoadFromDB_NotFound(t *testing.T) {
	gormDB, mock := newGuildDBMock(t)
	g := &GuildActor{GuildID: 999, db: gormDB}

	mock.ExpectQuery(`SELECT \* FROM "guild" WHERE "guild"."id" = \$1 ORDER BY "guild"."id" LIMIT \$2`).
		WithArgs(int64(999), 1).
		WillReturnError(gorm.ErrRecordNotFound)

	err := g.loadFromDB(context.Background())
	if err == nil {
		t.Fatal("expected error for missing guild")
	}
}

func TestGuildActor_Save(t *testing.T) {
	gormDB, mock := newGuildDBMock(t)
	g := &GuildActor{
		GuildID: 1,
		db:      gormDB,
		Data: &Guild{
			ID: 1, Name: "TestGuild", Level: 2,
			LeaderID: 100, MemberCount: 3,
			Members: []*GuildMember{{RoleID: 100, Position: 1, JoinedAt: 1000}},
			Version: 1,
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "guild" SET .* WHERE .*`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	g.save(context.Background())
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestGuildActor_Save_NilData(t *testing.T) {
	gormDB, mock := newGuildDBMock(t)
	g := &GuildActor{GuildID: 1, db: gormDB, Data: nil}

	g.save(context.Background())
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("nil data should not touch db: %v", err)
	}
}
