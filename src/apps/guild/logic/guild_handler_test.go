package logic

// guild 业务 handler 完整流程测试:职位/转让/信息/退出/解散/踢出/查询。
// 成员用负 RoleID(notifyPlayer → PublishRoleNotify 走 invalid 分支, 避开未初始化 Redis)。

import (
	"context"
	"errors"
	"testing"
	"time"

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"

	"github.com/DATA-DOG/go-sqlmock"
)

// newTestGuildNeg 负 RoleID 版本: 通知遍历成员时不触达全局 Redis。
func newTestGuildNeg(t *testing.T) *GuildActor {
	t.Helper()
	g := newTestGuild(t)
	for _, m := range g.Data.Members {
		m.RoleID = -m.RoleID
	}
	g.Data.LeaderID = -100
	return g
}

// withRolePublic 注入 getRolePublic fake(buildNotifyGuildInfo 需要)。
func withRolePublic(t *testing.T) {
	t.Helper()
	old := getRolePublic
	getRolePublic = func(ctx context.Context, roleID int64) *pb.PRolePublic {
		return &pb.PRolePublic{RoleId: roleID}
	}
	t.Cleanup(func() { getRolePublic = old })
}

// expectGuildSave 期望一次 gorm Save 事务(guild 表 UPDATE)。
func expectGuildSave(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "guild" SET .* WHERE .*`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

// expectRoleGuildReset 期望一次 role_guild 归属清零 UPDATE(包事务, 2 参数: SET 值 + WHERE role_id)。
func expectRoleGuildReset(mock sqlmock.Sqlmock, roleID int64) {
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "role_guild" SET .* WHERE role_id = \$2`).
		WithArgs(sqlmock.AnyArg(), roleID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

// ========== SetPosition ==========

func TestSetPosition(t *testing.T) {
	cases := []struct {
		name        string
		req         *pb.ReqGuildSetPosition
		wantErr     error
		wantSuccess bool
	}{
		{
			"success", &pb.ReqGuildSetPosition{
				RoleId: -100, TargetId: -300, Position: int32(gamecfg.GardenEGuildPosition_VICE_LEADER),
			}, nil, true,
		},
		{"permission denied", &pb.ReqGuildSetPosition{RoleId: -300, TargetId: -200, Position: int32(gamecfg.GardenEGuildPosition_MEMBER)}, ErrPermissionDenied, false},
		{"self", &pb.ReqGuildSetPosition{RoleId: -100, TargetId: -100, Position: int32(gamecfg.GardenEGuildPosition_VICE_LEADER)}, ErrCannotSetPositionToSelf, false},
		{"target not found", &pb.ReqGuildSetPosition{RoleId: -100, TargetId: -999, Position: int32(gamecfg.GardenEGuildPosition_VICE_LEADER)}, ErrMemberNotFound, false},
		// 99 非法且 > 会长职位(1): 过权限检查, 命中 InvalidPosition
		{"invalid position", &pb.ReqGuildSetPosition{RoleId: -100, TargetId: -300, Position: 99}, ErrInvalidPosition, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			initGuildTestConfig(t)
			withRolePublic(t)
			g := newTestGuildNeg(t)

			rsp, err := g.SetPosition(context.Background(), c.req)
			if c.wantSuccess {
				if err != nil {
					t.Fatalf("SetPosition: %v", err)
				}
				if rsp == nil {
					t.Fatal("nil rsp")
				}
				if g.getMember(-300).Position != int32(gamecfg.GardenEGuildPosition_VICE_LEADER) {
					t.Fatal("target position not updated")
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("expected %v, got %v", c.wantErr, err)
			}
		})
	}
}

// ========== TransferLeader ==========

func TestTransferLeader(t *testing.T) {
	cases := []struct {
		name        string
		req         *pb.ReqGuildTransferLeader
		wantErr     error
		wantSuccess bool
	}{
		{"success", &pb.ReqGuildTransferLeader{RoleId: -100, TargetId: -300}, nil, true},
		{"not leader", &pb.ReqGuildTransferLeader{RoleId: -200, TargetId: -300}, ErrPermissionDenied, false},
		{"self", &pb.ReqGuildTransferLeader{RoleId: -100, TargetId: -100}, ErrCannotTransferToSelf, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			initGuildTestConfig(t)
			withRolePublic(t)
			g := newTestGuildNeg(t)
			if c.wantSuccess {
				gormDB, mock := newGuildDBMock(t)
				g.db = gormDB
				expectGuildSave(mock) // addLog
			}

			_, err := g.TransferLeader(context.Background(), c.req)
			if !c.wantSuccess {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("expected %v, got %v", c.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("TransferLeader: %v", err)
			}
			if g.Data.LeaderID != -300 {
				t.Fatalf("expected new leader -300, got %d", g.Data.LeaderID)
			}
			if g.getMember(-100).Position != int32(gamecfg.GardenEGuildPosition_MEMBER) {
				t.Fatal("old leader should demote to member")
			}
			if g.getMember(-300).Position != int32(gamecfg.GardenEGuildPosition_LEADER) {
				t.Fatal("target should become leader")
			}
		})
	}
}

// ========== UpdateGuildInfo ==========

func TestUpdateGuildInfo_Success(t *testing.T) {
	initGuildTestConfig(t)
	withRolePublic(t)
	g := newTestGuildNeg(t)

	_, err := g.UpdateGuildInfo(context.Background(), &pb.ReqGuildUpdateInfo{
		RoleId: -100, Declaration: "新宣言", Announcement: "新公告", NeedApproval: false,
	})
	if err != nil {
		t.Fatalf("UpdateGuildInfo: %v", err)
	}
	if g.Data.Declaration != "新宣言" || g.Data.Announcement != "新公告" || g.Data.NeedApproval {
		t.Fatalf("guild info not updated: %+v", g.Data)
	}
}

func TestUpdateGuildInfo_MemberDenied(t *testing.T) {
	initGuildTestConfig(t)
	g := newTestGuildNeg(t)

	_, err := g.UpdateGuildInfo(context.Background(), &pb.ReqGuildUpdateInfo{
		RoleId: -300, Declaration: "x", NeedApproval: true,
	})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied, got %v", err)
	}
}

// ========== LeaveGuild ==========

func TestLeaveGuild_Success(t *testing.T) {
	initGuildTestConfig(t)
	withRolePublic(t)
	g := newTestGuildNeg(t)
	gormDB, mock := newGuildDBMock(t)
	g.db = gormDB

	expectRoleGuildReset(mock, -300)
	expectGuildSave(mock) // addLog

	_, err := g.LeaveGuild(context.Background(), &pb.ReqGuildLeave{RoleId: -300})
	if err != nil {
		t.Fatalf("LeaveGuild: %v", err)
	}
	if len(g.Data.Members) != 2 || g.Data.MemberCount != 2 {
		t.Fatalf("expected 2 members, got %d (count=%d)", len(g.Data.Members), g.Data.MemberCount)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestLeaveGuild_LeaderDenied(t *testing.T) {
	initGuildTestConfig(t)
	g := newTestGuildNeg(t)

	_, err := g.LeaveGuild(context.Background(), &pb.ReqGuildLeave{RoleId: -100})
	if err == nil {
		t.Fatal("expected leader leave error")
	}
}

func TestLeaveGuild_NotMember(t *testing.T) {
	initGuildTestConfig(t)
	g := newTestGuildNeg(t)

	_, err := g.LeaveGuild(context.Background(), &pb.ReqGuildLeave{RoleId: -999})
	if err == nil {
		t.Fatal("expected not-in-guild error")
	}
}

// ========== DisbandGuild ==========

func TestDisbandGuild_Denied(t *testing.T) {
	cases := []struct {
		name    string
		roleID  int64
		wantErr error
	}{
		{"not leader", -200, ErrPermissionDenied},
		{"has members", -100, ErrGuildHasMembers},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			initGuildTestConfig(t)
			g := newTestGuildNeg(t)

			_, err := g.DisbandGuild(context.Background(), &pb.ReqGuildDisband{RoleId: c.roleID})
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("expected %v, got %v", c.wantErr, err)
			}
		})
	}
}

func TestDisbandGuild_Success(t *testing.T) {
	initGuildTestConfig(t)
	withRolePublic(t)
	g := newTestGuildNeg(t)
	gormDB, mock := newGuildDBMock(t)
	g.db = gormDB
	// 只剩会长 1 人
	g.Data.Members = g.Data.Members[:1]
	g.Data.MemberCount = 1

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "guild"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "role_guild" SET .* WHERE guild_id = \$2`).
		WithArgs(sqlmock.AnyArg(), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	_, err := g.DisbandGuild(context.Background(), &pb.ReqGuildDisband{RoleId: -100})
	if err != nil {
		t.Fatalf("DisbandGuild: %v", err)
	}
	if !g.StopRequested() {
		t.Fatal("DisbandGuild must request actor termination")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

// ========== KickMember ==========

func TestKickMember_Success(t *testing.T) {
	initGuildTestConfig(t)
	withRolePublic(t)
	g := newTestGuildNeg(t)
	gormDB, mock := newGuildDBMock(t)
	g.db = gormDB

	expectRoleGuildReset(mock, -300)
	expectGuildSave(mock) // addLog

	if err := g.KickMember(context.Background(), -100, &pb.ReqGuildKickMember{
		TargetId: -300, Reason: "test",
	}); err != nil {
		t.Fatalf("KickMember: %v", err)
	}
	if len(g.Data.Members) != 2 {
		t.Fatalf("expected 2 members after kick, got %d", len(g.Data.Members))
	}
}

func TestKickMember_PermissionDenied(t *testing.T) {
	initGuildTestConfig(t)
	g := newTestGuildNeg(t)

	if err := g.KickMember(context.Background(), -300, &pb.ReqGuildKickMember{
		TargetId: -200, Reason: "x",
	}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied, got %v", err)
	}
}

// ========== 查询 ==========

func TestGuildInfo(t *testing.T) {
	initGuildTestConfig(t)
	withRolePublic(t)
	g := newTestGuildNeg(t)
	g.Data.Logs = []*GuildLog{{Content: "log1", CreatedAt: time.Now()}}

	rsp, err := g.GuildInfo(context.Background(), &pb.ReqGuildInfo{})
	if err != nil {
		t.Fatalf("GuildInfo: %v", err)
	}
	if rsp.Guild.Id != 1 || rsp.Guild.LeaderId != -100 {
		t.Fatalf("unexpected guild info: %+v", rsp.Guild)
	}
	if len(rsp.Members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(rsp.Members))
	}
	if len(rsp.Logs) != 1 || rsp.Logs[0].Content != "log1" {
		t.Fatalf("unexpected logs: %+v", rsp.Logs)
	}
}

func TestGetGuildApplyList(t *testing.T) {
	initGuildTestConfig(t)
	withRolePublic(t)
	g := newTestGuildNeg(t)
	g.Data.ApplyList = []*GuildApply{{ID: 1, RoleID: 500, Status: 0}}

	rsp, err := g.GetGuildApplyList(context.Background(), &pb.ReqGuildApplyList{})
	if err != nil {
		t.Fatalf("GetGuildApplyList: %v", err)
	}
	if len(rsp.Applies) != 1 || rsp.Applies[0].ApplyId != 1 {
		t.Fatalf("unexpected applies: %+v", rsp.Applies)
	}
}

// timeNow 避免直接依赖 time 构造(测试内嵌)。
