package logic

// guild 申请/审批/加入流程测试:ApplyGuild 分流、addMember 原子门、
// processSingleApply 审批。复用 guild_actor_test.go 的 newTestGuild/initGuildTestConfig。

import (
	"context"
	"errors"
	"testing"

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"

	"github.com/DATA-DOG/go-sqlmock"
)

// withFakeRolePublic 替换 getRolePublic, 避免依赖未初始化 Redis/DB。
func withFakeRolePublic(t *testing.T) {
	t.Helper()
	old := getRolePublic
	getRolePublic = func(ctx context.Context, roleID int64) *pb.PRolePublic {
		return &pb.PRolePublic{RoleId: roleID}
	}
	t.Cleanup(func() { getRolePublic = old })
}

// ========== ApplyGuild 分流 ==========

func TestApplyGuild_DuplicateApply(t *testing.T) {
	initGuildTestConfig(t)
	g := newTestGuild()
	g.Data.ApplyList = []*GuildApply{{ID: 1, RoleID: 400, Status: 0}}

	_, err := g.ApplyGuild(context.Background(), &pb.ReqGuildApply{RoleId: 400})
	if err == nil {
		t.Fatal("expected duplicate apply error")
	}
	if len(g.Data.ApplyList) != 1 {
		t.Fatalf("apply list must not grow, got %d", len(g.Data.ApplyList))
	}
}

func TestApplyGuild_NeedApproval_CreatesApply(t *testing.T) {
	initGuildTestConfig(t)
	withFakeRolePublic(t)
	g := newTestGuild()
	g.Data.NeedApproval = true
	g.Data.ApplyList = nil
	g.Data.Members = nil // 清空成员: notifyApplyUpdate 遍历成员会触达未初始化 Redis

	if _, err := g.ApplyGuild(context.Background(), &pb.ReqGuildApply{RoleId: 400}); err != nil {
		t.Fatalf("ApplyGuild: %v", err)
	}
	if len(g.Data.ApplyList) != 1 {
		t.Fatalf("expected 1 apply, got %d", len(g.Data.ApplyList))
	}
	a := g.Data.ApplyList[0]
	if a.RoleID != 400 || a.Status != 0 || a.ID <= 0 {
		t.Fatalf("unexpected apply: %+v", a)
	}
	if a.ExpireAt.IsZero() {
		t.Fatal("ExpireAt not set")
	}
}

func TestApplyGuild_DirectJoin(t *testing.T) {
	initGuildTestConfig(t)
	g := newTestGuild()
	g.Data.NeedApproval = false

	// NeedApproval=false 应走 joinDirect(而非 createApply):
	// addMember 返回"已入会"错误 → ApplyGuild 报错; 若误走 createApply 则
	// ApplyList 会增长且不报错(区分度)。成功加入路径由 TestAddMember_Success 覆盖。
	gormDB, mock := newGuildDBMock(t)
	g.db = gormDB
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "role_guild"`).
		WithArgs(int64(1), int64(400), int64(1), int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{})) // RowsAffected=0 → 已入会
	mock.ExpectCommit()

	_, err := g.ApplyGuild(context.Background(), &pb.ReqGuildApply{RoleId: 400})
	if !errors.Is(err, ErrPlayerAlreadyInGuild) {
		t.Fatalf("expected ErrPlayerAlreadyInGuild (joinDirect path), got %v", err)
	}
	if len(g.Data.ApplyList) != 0 {
		t.Fatalf("joinDirect must not create apply, got %d applies", len(g.Data.ApplyList))
	}
}

// ========== addMember 原子门 ==========

func TestAddMember_AlreadyInGuild(t *testing.T) {
	initGuildTestConfig(t)
	g := newTestGuild()
	gormDB, mock := newGuildDBMock(t)
	g.db = gormDB

	// ON CONFLICT RowsAffected=0 → 已入会
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "role_guild"`).
		WithArgs(int64(1), int64(400), int64(1), int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{}))
	mock.ExpectCommit()

	if err := g.addMember(context.Background(), 400); !errors.Is(err, ErrPlayerAlreadyInGuild) {
		t.Fatalf("expected ErrPlayerAlreadyInGuild, got %v", err)
	}
	if len(g.Data.Members) != 3 {
		t.Fatalf("members must not change, got %d", len(g.Data.Members))
	}
}

func TestAddMember_GuildFull(t *testing.T) {
	initGuildTestConfig(t)
	g := newTestGuild()
	// 填满成员到 MemberLimit(配表 Level 1 上限)
	limit := int(g.cfg.TbGuildLevel.Get(g.Data.Level).MemberLimit)
	for i := 0; i < limit; i++ {
		g.Data.Members = append(g.Data.Members, &GuildMember{RoleID: int64(1000 + i)})
	}

	if err := g.addMember(context.Background(), 400); !errors.Is(err, ErrGuildFull) {
		t.Fatalf("expected ErrGuildFull, got %v", err)
	}
}

func TestAddMember_Success(t *testing.T) {
	initGuildTestConfig(t)
	g := newTestGuild()
	gormDB, mock := newGuildDBMock(t)
	g.db = gormDB

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "role_guild"`).
		WithArgs(int64(1), int64(400), int64(1), int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{"role_id", "guild_id"}).AddRow(400, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "guild" SET .* WHERE .*`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := g.addMember(context.Background(), 400); err != nil {
		t.Fatalf("addMember: %v", err)
	}
	if len(g.Data.Members) != 4 || g.Data.MemberCount != 4 {
		t.Fatalf("expected 4 members, got %d (count=%d)", len(g.Data.Members), g.Data.MemberCount)
	}
	last := g.Data.Members[len(g.Data.Members)-1]
	if last.RoleID != 400 || last.Position != int32(gamecfg.GardenEGuildPosition_MEMBER) {
		t.Fatalf("unexpected new member: %+v", last)
	}
	if len(g.Data.Logs) == 0 {
		t.Fatal("expected join log")
	}
}

// ========== processSingleApply ==========

func TestProcessSingleApply_Reject(t *testing.T) {
	initGuildTestConfig(t)
	g := newTestGuild()
	g.Data.ApplyList = []*GuildApply{{ID: 5, RoleID: 400, Status: 0}}

	if err := g.processSingleApply(context.Background(), 5, false); err != nil {
		t.Fatalf("processSingleApply: %v", err)
	}
	if g.Data.ApplyList[0].Status != 2 {
		t.Fatalf("expected rejected status 2, got %d", g.Data.ApplyList[0].Status)
	}
	if len(g.Data.Members) != 3 {
		t.Fatalf("reject must not add member, got %d", len(g.Data.Members))
	}
}

func TestProcessSingleApply_Approve(t *testing.T) {
	initGuildTestConfig(t)
	g := newTestGuild()
	gormDB, mock := newGuildDBMock(t)
	g.db = gormDB
	g.Data.ApplyList = []*GuildApply{{ID: 5, RoleID: 400, Status: 0}}

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "role_guild"`).
		WithArgs(int64(1), int64(400), int64(1), int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{"role_id", "guild_id"}).AddRow(400, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "guild" SET .* WHERE .*`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := g.processSingleApply(context.Background(), 5, true); err != nil {
		t.Fatalf("processSingleApply: %v", err)
	}
	if g.Data.ApplyList[0].Status != 1 {
		t.Fatalf("expected approved status 1, got %d", g.Data.ApplyList[0].Status)
	}
	if len(g.Data.Members) != 4 {
		t.Fatalf("expected 4 members after approve, got %d", len(g.Data.Members))
	}
}

func TestProcessSingleApply_NotFound(t *testing.T) {
	initGuildTestConfig(t)
	g := newTestGuild()

	if err := g.processSingleApply(context.Background(), 999, true); !errors.Is(err, ErrApplyExpired) {
		t.Fatalf("expected ErrApplyExpired, got %v", err)
	}
}

// ========== ApproveApply 批量部分成功 ==========

// TestApproveApply_MixedSkipsFailed 批量中混有不存在的 ID:
// 跳过后继续处理有效 ID, 成功项生效。
func TestApproveApply_MixedSkipsFailed(t *testing.T) {
	initGuildTestConfig(t)
	withFakeRolePublic(t)
	g := newTestGuildNeg(t) // 负 RoleID 成员: notify 遍历避开未初始化 Redis
	gormDB, mock := newGuildDBMock(t)
	g.db = gormDB
	g.Data.ApplyList = []*GuildApply{{ID: 5, RoleID: -400, Status: 0}}

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "role_guild"`).
		WithArgs(int64(1), int64(-400), int64(1), int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{"role_id", "guild_id"}).AddRow(-400, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "guild" SET .* WHERE .*`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// [0(不存在), 5(有效)]: 0 跳过, 5 处理成功
	err := g.ApproveApply(context.Background(), -100, &pb.ReqGuildApproveApply{
		ApplyIds: []int64{0, 5}, Approve: true,
	})
	if err != nil {
		t.Fatalf("ApproveApply should not fail on skipped id, got %v", err)
	}
	if g.Data.ApplyList[0].Status != 1 {
		t.Fatalf("expected apply 5 approved, got status %d", g.Data.ApplyList[0].Status)
	}
	if len(g.Data.Members) != 4 {
		t.Fatalf("expected member added, got %d", len(g.Data.Members))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
