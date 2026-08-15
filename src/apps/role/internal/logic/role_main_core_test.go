package logic

// role_main 核心状态机与持久化逻辑测试:状态门控、登出原因分类、
// saveRoleModuleState 版本冲突检测、loadModuleState、指标标签。

import (
	"context"
	"errors"
	"testing"

	"gserver/core/gxymodule"
	"gserver/core/gxynet/codec"
	"gserver/protocol/pb"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gogf/gf/v2/errors/gerror"
	"google.golang.org/protobuf/types/known/anypb"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ========== canHandleMsg 状态机 ==========

func TestCanHandleMsg(t *testing.T) {
	login := &pb.ReqAccountLogin{}
	normal := &pb.ReqAccountLogout{}

	// 登录消息: 仅 init 状态拒绝(已初始化/已登录可重复登录)
	if canHandleMsg(RoleStateInit, login) {
		t.Fatal("login msg must be rejected in init state")
	}
	if !canHandleMsg(RoleStateLoad, login) {
		t.Fatal("login msg must be accepted in load state")
	}
	if !canHandleMsg(RoleStateLogined, login) {
		t.Fatal("login msg must be accepted in logined state (re-login)")
	}
	// 普通消息: 仅 logined 状态接受
	if canHandleMsg(RoleStateInit, normal) {
		t.Fatal("normal msg must be rejected in init state")
	}
	if canHandleMsg(RoleStateLoad, normal) {
		t.Fatal("normal msg must be rejected in load state")
	}
	if !canHandleMsg(RoleStateLogined, normal) {
		t.Fatal("normal msg must be accepted in logined state")
	}
	if canHandleMsg(RoleStateLogout, normal) {
		t.Fatal("normal msg must be rejected in logout state")
	}
}

// ========== roleLogoutReason ==========

func TestRoleLogoutReason(t *testing.T) {
	cases := []struct {
		reason string
		want   string
	}{
		{"client account logout", "client_logout"},
		{"session alive timeout", "session_alive_timeout"},
		{"session terminated", "session_terminated"},
		{"Client Account Logout", "client_logout"}, // 大小写不敏感
		{"some other reason", "unknown"},
		{"", "unknown"},
	}
	for _, c := range cases {
		if got := roleLogoutReason(c.reason); got != c.want {
			t.Fatalf("roleLogoutReason(%q) = %q, want %q", c.reason, got, c.want)
		}
	}
}

// ========== saveRoleModuleState 版本冲突检测 ==========

// corePersistState 常量 TableName(gorm 对指针接收者+字段 TableName 解析失败,
// 见 role_save_test.go 的 testPersistState——仅用于非 gorm 场景)。
type corePersistState struct {
	RolePersistState
}

func (corePersistState) TableName() string { return "test_mod" }

type coreRoleModule struct {
	gxymodule.ModuleBase
	state *corePersistState
}

func (m *coreRoleModule) SetRole(role *RoleMain)         {}
func (m *coreRoleModule) OnCreate(ctx context.Context)   {}
func (m *coreRoleModule) AfterLogin(ctx context.Context) {}
func (m *coreRoleModule) BeforeLogout(ctx context.Context) {
}
func (m *coreRoleModule) PersistState() IPersistState {
	if m.state == nil {
		return nil
	}
	return m.state
}

func newGormDBForRole(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	return db, mock
}

func newRoleMainForSave(roleID int64) *RoleMain {
	r := NewRoleMain()
	r.RoleID = roleID
	return r
}

// TestSaveRoleModuleState_NoState 无持久化状态的模块直接跳过。
func TestSaveRoleModuleState_NoState(t *testing.T) {
	db, mock := newGormDBForRole(t)
	r := newRoleMainForSave(100)
	saved, err := r.saveRoleModuleState(context.Background(), db, &coreRoleModule{})
	if err != nil {
		t.Fatalf("saveRoleModuleState: %v", err)
	}
	if saved != nil {
		t.Fatalf("expected nil saved, got %+v", saved)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected db calls: %v", err)
	}
}

// TestSaveRoleModuleState_NotDirty 未脏标记跳过写库。
func TestSaveRoleModuleState_NotDirty(t *testing.T) {
	db, mock := newGormDBForRole(t)
	r := newRoleMainForSave(100)
	st := &corePersistState{}
	saved, err := r.saveRoleModuleState(context.Background(), db, &coreRoleModule{state: st})
	if err != nil {
		t.Fatalf("saveRoleModuleState: %v", err)
	}
	if saved != nil {
		t.Fatalf("expected nil saved, got %+v", saved)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected db calls: %v", err)
	}
}

// TestSaveRoleModuleState_NewRowVersion0 version=0(新号)走 INSERT。
func TestSaveRoleModuleState_NewRowVersion0(t *testing.T) {
	db, mock := newGormDBForRole(t)
	r := newRoleMainForSave(100)
	st := &corePersistState{}
	st.MarkDirty()
	st.Version = 0

	mock.ExpectBegin()
	// gorm Save 主键零值 → INSERT ... RETURNING role_id(走 Query)
	mock.ExpectQuery(`INSERT INTO "test_mod"`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"role_id"}).AddRow(100))
	mock.ExpectCommit()

	saved, err := r.saveRoleModuleState(context.Background(), db, &coreRoleModule{state: st})
	if err != nil {
		t.Fatalf("saveRoleModuleState: %v", err)
	}
	if saved == nil {
		t.Fatal("expected saved result for new row")
	}
	if saved.versionChanged {
		t.Fatal("new row insert must not be versionChanged")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations: %v", err)
	}
}

// TestSaveRoleModuleState_ConflictRollback version>0 但 RowsAffected=0:
// 冲突, 版本回滚, 不清 dirty。
func TestSaveRoleModuleState_ConflictRollback(t *testing.T) {
	db, mock := newGormDBForRole(t)
	r := newRoleMainForSave(100)
	st := &corePersistState{}
	st.MarkDirty()
	st.Version = 5

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "test_mod"`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	saved, err := r.saveRoleModuleState(context.Background(), db, &coreRoleModule{state: st})
	if err != nil {
		t.Fatalf("saveRoleModuleState: %v", err)
	}
	if saved != nil {
		t.Fatalf("expected nil saved on conflict, got %+v", saved)
	}
	if st.Version != 5 {
		t.Fatalf("version must roll back to 5 on conflict, got %d", st.Version)
	}
	if !st.IsDirty() {
		t.Fatal("dirty flag must survive conflict for retry")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations: %v", err)
	}
}

// TestSaveRoleModuleState_Success version>0 且冲突检测通过: version+1。
func TestSaveRoleModuleState_Success(t *testing.T) {
	db, mock := newGormDBForRole(t)
	r := newRoleMainForSave(100)
	st := &corePersistState{}
	st.MarkDirty()
	st.Version = 5

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "test_mod"`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	saved, err := r.saveRoleModuleState(context.Background(), db, &coreRoleModule{state: st})
	if err != nil {
		t.Fatalf("saveRoleModuleState: %v", err)
	}
	if saved == nil {
		t.Fatal("expected saved result")
	}
	if !saved.versionChanged || saved.oldVersion != 5 {
		t.Fatalf("expected versionChanged with oldVersion=5, got %+v", saved)
	}
	if st.Version != 6 {
		t.Fatalf("expected version 6, got %d", st.Version)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations: %v", err)
	}
}

// ========== loadModuleState ==========

// TestLoadModuleState_NotFound 未找到记录: 设置 RoleID, 不报错。
func TestLoadModuleState_NotFound(t *testing.T) {
	db, mock := newGormDBForRole(t)
	mock.ExpectQuery(`SELECT .* FROM "test_mod"`).
		WillReturnError(gorm.ErrRecordNotFound)

	st := &corePersistState{}
	if err := loadModuleState(context.Background(), db, 42, st); err != nil {
		t.Fatalf("loadModuleState: %v", err)
	}
	if st.RoleID != 42 {
		t.Fatalf("expected RoleID 42 set, got %d", st.RoleID)
	}
}

// TestLoadModuleState_Found 已有记录: 正常加载。
func TestLoadModuleState_Found(t *testing.T) {
	db, mock := newGormDBForRole(t)
	mock.ExpectQuery(`SELECT .* FROM "test_mod"`).
		WillReturnRows(sqlmock.NewRows([]string{"role_id", "version"}).
			AddRow(42, 7))

	st := &corePersistState{}
	if err := loadModuleState(context.Background(), db, 42, st); err != nil {
		t.Fatalf("loadModuleState: %v", err)
	}
	if st.Version != 7 {
		t.Fatalf("expected version 7 loaded, got %d", st.Version)
	}
}

// TestLoadModuleState_DBError 其他错误原样返回。
func TestLoadModuleState_DBError(t *testing.T) {
	db, mock := newGormDBForRole(t)
	mock.ExpectQuery(`SELECT .* FROM "test_mod"`).
		WillReturnError(errors.New("db down"))

	st := &corePersistState{}
	if err := loadModuleState(context.Background(), db, 42, st); err == nil {
		t.Fatal("expected db error propagated")
	}
}

// ========== clientMessageMetricLabels ==========

func TestClientMessageMetricLabels(t *testing.T) {
	// 已知消息: codec meta 提供 ID/Name
	known := &pb.RspAccountLogin{}
	meta := codec.MessageMetaByMsg(known)
	if meta == nil {
		t.Skip("RspAccountLogin has no codec meta registered")
	}
	id, name := clientMessageMetricLabels("", known)
	if id != meta.ID || name != meta.Name {
		t.Fatalf("known msg labels = (%q,%q), want (%q,%q)", id, name, meta.ID, meta.Name)
	}
}

func TestClientMessageMetricLabels_UnknownMsg(t *testing.T) {
	// 无 codec meta 的消息: 保留传入 id, name 用 descriptor
	type unknownProto struct{ pb.ActorError }
	msg := &unknownProto{}
	if meta := codec.MessageMetaByMsg(msg); meta != nil {
		t.Skipf("unexpected meta for unknown type: %+v", meta)
	}
	id, name := clientMessageMetricLabels("orig_id", msg)
	if id != "orig_id" {
		t.Fatalf("expected id preserved, got %q", id)
	}
	wantName := string(msg.ProtoReflect().Descriptor().Name())
	if name != wantName {
		t.Fatalf("expected name %q, got %q", wantName, name)
	}
}

// 辅助: 确保 gerror/anypb 引用(避免误删 import 后编译不过)。
var _ = gerror.New
var _ = anypb.Any{}
