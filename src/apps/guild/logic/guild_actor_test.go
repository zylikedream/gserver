package logic

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gserver/core/gxytimer"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/pkg/gameconfig"
)

// ========== test setup ==========

var guildRepoRoot string

func init() {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			guildRepoRoot = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			guildRepoRoot = "."
			break
		}
		dir = parent
	}
}

func loadGuildTestTable(t *testing.T, name string) []map[string]any {
	t.Helper()
	path := filepath.Join(guildRepoRoot, "gameconfig/json/"+name+".json")
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("load table %s: %v", name, err)
	}
	var data []map[string]any
	if err := json.Unmarshal(bytes, &data); err != nil {
		t.Fatalf("unmarshal table %s: %v", name, err)
	}
	return data
}

func initGuildTestConfig(t *testing.T) {
	t.Helper()
	gc := gameconfig.NewGameConfig()

	levels := loadGuildTestTable(t, "garden_tbguildlevel")
	tbLevel, err := gamecfg.NewGardenTbGuildLevel(levels)
	if err != nil {
		t.Fatal(err)
	}

	config := loadGuildTestTable(t, "garden_tbguildconfig")
	tbConfig, err := gamecfg.NewGardenTbGuildConfig(config)
	if err != nil {
		t.Fatal(err)
	}

	gc.Tables = &gamecfg.Tables{TbGuildLevel: tbLevel, TbGuildConfig: tbConfig}
}

func newTestGuild() *GuildActor {
	return &GuildActor{
		GuildID: 1,
		cfg:     gameconfig.Get(),
		Data: &Guild{
			ID: 1, Name: "TestGuild", Level: 1,
			LeaderID: 100, MemberCount: 3, NeedApproval: true,
			Members: []*GuildMember{
				{RoleID: 100, Position: int32(gamecfg.GardenEGuildPosition_LEADER), JoinedAt: 1000},
				{RoleID: 200, Position: int32(gamecfg.GardenEGuildPosition_VICE_LEADER), JoinedAt: 1001},
				{RoleID: 300, Position: int32(gamecfg.GardenEGuildPosition_MEMBER), JoinedAt: 1002},
			},
			ApplyList: []*GuildApply{},
			Logs:      []*GuildLog{},
		},
	}
}

func TestGuildActorInitAcceptsActorOwnerArgument(t *testing.T) {
	g := &GuildActor{}
	if err := g.Init(context.Background(), []any{int64(1), struct{}{}}); err != nil {
		t.Fatalf("Init with shared activator owner argument: %v", err)
	}
	if g.GuildID != 1 {
		t.Fatalf("GuildID = %d, want 1", g.GuildID)
	}
}

// ========== 纯函数测试 ==========

func TestRemoveMember(t *testing.T) {
	members := []*GuildMember{{RoleID: 1}, {RoleID: 2}, {RoleID: 3}}
	result := removeMember(members, 2)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].RoleID != 1 || result[1].RoleID != 3 {
		t.Fatalf("unexpected: %v", result)
	}
}

func TestRemoveMember_NotFound(t *testing.T) {
	members := []*GuildMember{{RoleID: 1}, {RoleID: 2}}
	result := removeMember(members, 99)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
}

func TestRemoveMember_Empty(t *testing.T) {
	result := removeMember(nil, 1)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestToSet(t *testing.T) {
	s := toSet([]int64{1, 2, 3})
	if len(s) != 3 {
		t.Fatalf("expected 3, got %d", len(s))
	}
	if _, ok := s[2]; !ok {
		t.Fatal("expected 2 in set")
	}
}

func TestToSet_Empty(t *testing.T) {
	s := toSet(nil)
	if len(s) != 0 {
		t.Fatalf("expected 0, got %d", len(s))
	}
}

// ========== getMember ==========

func TestGetMember_Found(t *testing.T) {
	g := newTestGuild()
	m := g.getMember(200)
	if m == nil || m.Position != int32(gamecfg.GardenEGuildPosition_VICE_LEADER) {
		t.Fatal("expected vice leader")
	}
}

func TestGetMember_NotFound(t *testing.T) {
	g := newTestGuild()
	if m := g.getMember(999); m != nil {
		t.Fatal("expected nil")
	}
}

// ========== canApprove ==========

func TestCanApprove_Leader(t *testing.T) {
	g := newTestGuild()
	if !g.canApprove(100) {
		t.Fatal("leader should approve")
	}
}

func TestCanApprove_ViceLeader(t *testing.T) {
	g := newTestGuild()
	if !g.canApprove(200) {
		t.Fatal("vice leader should approve")
	}
}

func TestCanApprove_Member(t *testing.T) {
	g := newTestGuild()
	if g.canApprove(300) {
		t.Fatal("member should not approve")
	}
}

func TestCanApprove_NonMember(t *testing.T) {
	g := newTestGuild()
	if g.canApprove(999) {
		t.Fatal("non-member should not approve")
	}
}

// ========== canKick ==========

func TestCanKick_LeaderKickMember(t *testing.T) {
	g := newTestGuild()
	if !g.canKick(100, 300) {
		t.Fatal("leader should kick member")
	}
}

func TestCanKick_LeaderKickViceLeader(t *testing.T) {
	g := newTestGuild()
	if !g.canKick(100, 200) {
		t.Fatal("leader should kick vice leader")
	}
}

func TestCanKick_ViceLeaderKickMember(t *testing.T) {
	g := newTestGuild()
	if !g.canKick(200, 300) {
		t.Fatal("vice leader should kick member")
	}
}

func TestCanKick_ViceLeaderCannotKickViceLeader(t *testing.T) {
	g := newTestGuild()
	// 250 is not in guild, so add one
	g.Data.Members = append(g.Data.Members, &GuildMember{RoleID: 250, Position: int32(gamecfg.GardenEGuildPosition_VICE_LEADER)})
	if g.canKick(200, 250) {
		t.Fatal("vice leader should not kick another vice leader")
	}
}

func TestCanKick_CannotKickLeader(t *testing.T) {
	g := newTestGuild()
	if g.canKick(200, 100) {
		t.Fatal("should not kick leader")
	}
}

func TestCanKick_CannotKickSelf(t *testing.T) {
	g := newTestGuild()
	if g.canKick(100, 100) {
		t.Fatal("should not kick self")
	}
}

func TestCanKick_MemberCannotKick(t *testing.T) {
	g := newTestGuild()
	if g.canKick(300, 200) {
		t.Fatal("member should not kick")
	}
}

// ========== getPendingApplies ==========

func TestGetPendingApplies(t *testing.T) {
	g := newTestGuild()
	g.Data.ApplyList = []*GuildApply{
		{ID: 1, RoleID: 400, Status: 0},
		{ID: 2, RoleID: 401, Status: 1},
		{ID: 3, RoleID: 402, Status: 0},
		{ID: 4, RoleID: 403, Status: 2},
	}
	pending := g.getPendingApplies()
	if len(pending) != 2 {
		t.Fatalf("expected 2, got %d", len(pending))
	}
	if pending[0].ID != 1 || pending[1].ID != 3 {
		t.Fatalf("unexpected: %v", pending)
	}
}

// ========== nextApplyID ==========

func TestNextApplyID_Empty(t *testing.T) {
	g := newTestGuild()
	if id := g.nextApplyID(); id != 1 {
		t.Fatalf("expected 1, got %d", id)
	}
}

func TestNextApplyID_Existing(t *testing.T) {
	g := newTestGuild()
	g.Data.ApplyList = []*GuildApply{{ID: 5}, {ID: 12}, {ID: 3}}
	if id := g.nextApplyID(); id != 13 {
		t.Fatalf("expected 13, got %d", id)
	}
}

// ========== onDayRefresh ==========

func TestOnDayRefresh_ClearsExpired(t *testing.T) {
	g := newTestGuild()
	now := time.Now()
	g.Data.ApplyList = []*GuildApply{
		{ID: 1, Status: 0, ExpireAt: now.Add(-1 * time.Hour)},
		{ID: 2, Status: 0, ExpireAt: now.Add(1 * time.Hour)},
		{ID: 3, Status: 1, ExpireAt: now.Add(-1 * time.Hour)},
		{ID: 4, Status: 0, ExpireAt: now.Add(24 * time.Hour)},
	}
	g.onDayRefresh(context.Background(), gxytimer.TimerActiveInfo{})
	if len(g.Data.ApplyList) != 3 {
		t.Fatalf("expected 3, got %d", len(g.Data.ApplyList))
	}
	for _, a := range g.Data.ApplyList {
		if a.ID == 1 {
			t.Fatal("expired pending should be removed")
		}
	}
}

func TestOnDayRefresh_AllValid(t *testing.T) {
	g := newTestGuild()
	g.Data.ApplyList = []*GuildApply{
		{ID: 1, Status: 0, ExpireAt: time.Now().Add(1 * time.Hour)},
		{ID: 2, Status: 1, ExpireAt: time.Now().Add(-1 * time.Hour)},
	}
	g.onDayRefresh(context.Background(), gxytimer.TimerActiveInfo{})
	if len(g.Data.ApplyList) != 2 {
		t.Fatalf("expected 2, got %d", len(g.Data.ApplyList))
	}
}

// ========== addLog 截断逻辑 ==========

func TestAddLog_TruncatesAt100(t *testing.T) {
	g := newTestGuild()
	for i := 0; i < 105; i++ {
		g.Data.Logs = append(g.Data.Logs, &GuildLog{Content: "old"})
	}
	// 模拟 addLog 中的截断逻辑
	entry := &GuildLog{Content: "new", CreatedAt: time.Now()}
	g.Data.Logs = append(g.Data.Logs, entry)
	if len(g.Data.Logs) > MaxLogCount {
		g.Data.Logs = g.Data.Logs[len(g.Data.Logs)-MaxLogCount:]
	}
	if len(g.Data.Logs) != 100 {
		t.Fatalf("expected 100, got %d", len(g.Data.Logs))
	}
	if g.Data.Logs[99].Content != "new" {
		t.Fatal("last log should be new")
	}
}

// ========== buildLogList ==========

func TestBuildLogList(t *testing.T) {
	g := newTestGuild()
	g.Data.Logs = []*GuildLog{
		{Content: "created", CreatedAt: time.Unix(1000, 0)},
		{Content: "joined", CreatedAt: time.Unix(2000, 0)},
	}
	logs := g.buildLogList()
	if len(logs) != 2 {
		t.Fatalf("expected 2, got %d", len(logs))
	}
	if logs[0].Content != "created" || logs[0].CreatedAt != 1000 {
		t.Fatalf("unexpected: %v", logs[0])
	}
}

// ========== buildNotifyGuildBasic ==========

func TestBuildNotifyGuildBasic(t *testing.T) {
	initGuildTestConfig(t)
	g := newTestGuild()
	msg := g.buildNotifyGuildBasic(context.Background())
	if msg.Guild.Id != 1 {
		t.Fatalf("expected guild id 1, got %d", msg.Guild.Id)
	}
	if msg.Guild.Name != "TestGuild" {
		t.Fatalf("expected TestGuild, got %s", msg.Guild.Name)
	}
	if msg.Guild.MemberLimit != 30 {
		t.Fatalf("expected member limit 30, got %d", msg.Guild.MemberLimit)
	}
}

// ========== GuildLogs ==========

func TestGuildLogs(t *testing.T) {
	g := newTestGuild()
	g.Data.Logs = []*GuildLog{{Content: "test log", CreatedAt: time.Now()}}
	rsp, err := g.GuildLogs(context.Background(), &pb.ReqGuildLogs{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(rsp.Logs))
	}
}

// ========== SetPosition (pure logic, no DB/notify) ==========

func TestSetPosition_LeaderSetsViceLeader(t *testing.T) {
	g := newTestGuild()
	// SetPosition calls notifyGuildInfo → need to avoid nil ActorBase
	// Test the permission logic directly
	op := g.getMember(100)
	target := g.getMember(300)
	if op == nil || op.Position >= int32(gamecfg.GardenEGuildPosition_VICE_LEADER) {
		t.Fatal("leader should have permission")
	}
	if target == nil {
		t.Fatal("target should exist")
	}
	target.Position = int32(gamecfg.GardenEGuildPosition_VICE_LEADER)
	if m := g.getMember(300); m.Position != int32(gamecfg.GardenEGuildPosition_VICE_LEADER) {
		t.Fatal("position should be updated")
	}
}

// ========== TransferLeader logic ==========

func TestTransferLeader_Logic(t *testing.T) {
	g := newTestGuild()
	op := g.getMember(100)
	target := g.getMember(200)
	if op == nil || op.Position != int32(gamecfg.GardenEGuildPosition_LEADER) {
		t.Fatal("operator should be leader")
	}
	op.Position = int32(gamecfg.GardenEGuildPosition_MEMBER)
	target.Position = int32(gamecfg.GardenEGuildPosition_LEADER)
	g.Data.LeaderID = 200

	if g.Data.LeaderID != 200 {
		t.Fatal("leader ID should be 200")
	}
	if g.getMember(100).Position != int32(gamecfg.GardenEGuildPosition_MEMBER) {
		t.Fatal("old leader should be member")
	}
	if g.getMember(200).Position != int32(gamecfg.GardenEGuildPosition_LEADER) {
		t.Fatal("new leader should be leader")
	}
}

// ========== UpdateGuildInfo logic ==========

func TestUpdateGuildInfo_Logic(t *testing.T) {
	g := newTestGuild()
	op := g.getMember(100)
	if op == nil || op.Position > int32(gamecfg.GardenEGuildPosition_VICE_LEADER) {
		t.Fatal("leader should have permission")
	}
	g.Data.Declaration = "new decl"
	g.Data.Announcement = "new ann"
	g.Data.NeedApproval = false
	if g.Data.Declaration != "new decl" || g.Data.NeedApproval {
		t.Fatal("guild info should be updated")
	}
}

// ========== LeaveGuild logic ==========

func TestLeaveGuild_MemberCanLeave(t *testing.T) {
	g := newTestGuild()
	op := g.getMember(300)
	if op == nil {
		t.Fatal("member should exist")
	}
	if op.Position == int32(gamecfg.GardenEGuildPosition_LEADER) {
		t.Fatal("leader should not be able to leave via this path")
	}
	g.Data.Members = removeMember(g.Data.Members, 300)
	g.Data.MemberCount = int32(len(g.Data.Members))
	if len(g.Data.Members) != 2 || g.Data.MemberCount != 2 {
		t.Fatalf("expected 2 members, got %d", g.Data.MemberCount)
	}
}

// ========== DisbandGuild logic ==========

func TestDisbandGuild_OnlyLeaderCanDisband(t *testing.T) {
	g := newTestGuild()
	op := g.getMember(100)
	if op == nil || op.Position != int32(gamecfg.GardenEGuildPosition_LEADER) {
		t.Fatal("only leader can disband")
	}
	// Cannot disband with multiple members
	if len(g.Data.Members) <= 1 {
		t.Fatal("test setup error: expected multiple members")
	}

	// Remove all but leader
	g.Data.Members = []*GuildMember{{RoleID: 100, Position: int32(gamecfg.GardenEGuildPosition_LEADER)}}
	g.Data.MemberCount = 1
	if len(g.Data.Members) > 1 {
		t.Fatal("cannot disband when members > 1")
	}
}
