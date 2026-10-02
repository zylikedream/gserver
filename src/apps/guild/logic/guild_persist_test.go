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

// TestGuildActor_LoadFromDB:loadFromDB 必须把 members 这类 jsonb 列反序列化成
// *GuildMember 切片,并只按主键取单行。为什么需要:addMember/canApprove/LeaveGuild
// 全部在内存 Data 上判定;jsonb 反序列化一旦失效,公会不会报错,只会所有人静默变成"非成员"。
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

// TestGuildActor_LoadFromDB_NotFound:加载失败时 Data 必须保持 nil(赋值在 err 检查之后)。
// 为什么需要:Terminate 无条件调用 save,而 save 只按 Data==nil 判空;若失败路径留下空
// 对象,进程退出时就会把一条空公会行 UPDATE 回数据库,覆盖真实数据。
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
	// 加载失败不得留下空数据:否则终止路径会把它写回数据库。
	if g.Data != nil {
		t.Fatalf("Data must stay nil when load fails, got %+v", g.Data)
	}
}

// TestGuildActor_Save:落盘必须是单事务的 UPDATE(不是 INSERT),由 TickSave/OnModStop 共用。
// 为什么需要:改成 INSERT 会在重启后产生重复行;拆掉事务则中断时留下半写状态,内存与数据库静默分叉。
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

// TestGuildActor_TerminatePersists:正常停止路径必须先落盘再由门面释放归属(不变量 #4)。
// 为什么需要:省掉这次落盘时,进程收到的最后一段内存变更(职位/成员/日志)永久丢失,
// 且没有任何报错——玩家只会看到公会状态莫名回退。
func TestGuildActor_TerminatePersists(t *testing.T) {
	// 走真实构造路径:验证终止路径确实接上了落盘。
	g := NewGuildActor()
	g.GuildID = 1
	g.Data = &Guild{
		ID: 1, Name: "TestGuild", Level: 2, LeaderID: 100, MemberCount: 1,
		Members: []*GuildMember{{RoleID: 100, Position: 1, JoinedAt: 1000}},
	}
	gormDB, mock := newGuildDBMock(t)
	g.db = gormDB

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "guild" SET .* WHERE .*`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	g.Terminate(nil)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("actor 停止时必须落盘: %v", err)
	}
}

// TestGuildActor_Save_NilData:Data 为 nil 时 save 必须完全不发 SQL。
// 为什么需要:这是 NotFound 那条不变量的执行点;没有它,加载失败的 actor 停止时会
// 用空对象把数据库里的真实行覆盖成空行。
func TestGuildActor_Save_NilData(t *testing.T) {
	gormDB, mock := newGuildDBMock(t)
	g := &GuildActor{GuildID: 1, db: gormDB, Data: nil}

	g.save(context.Background())
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("nil data should not touch db: %v", err)
	}
}
