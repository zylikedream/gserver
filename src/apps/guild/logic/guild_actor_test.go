package logic

import (
	"context"
	"encoding/json"
	"gserver/src/lib"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gserver/core/gxyactor/gxyactortest"
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

// newTestGuild 在 mock 节点上创建真实 guild actor,并注入测试数据。
func newTestGuild(t *testing.T) *GuildActor {
	t.Helper()
	gxyactortest.StubOwnership(t)
	g, _ := gxyactortest.Spawn(t, lib.GUILD_ACTOR_TYPE, NewGuildActor, int64(1))
	g.Data = &Guild{
		ID: 1, Name: "TestGuild", Level: 1,
		LeaderID: 100, MemberCount: 3, NeedApproval: true,
		Members: []*GuildMember{
			{RoleID: 100, Position: int32(gamecfg.GardenEGuildPosition_LEADER), JoinedAt: 1000},
			{RoleID: 200, Position: int32(gamecfg.GardenEGuildPosition_VICE_LEADER), JoinedAt: 1001},
			{RoleID: 300, Position: int32(gamecfg.GardenEGuildPosition_MEMBER), JoinedAt: 1002},
		},
		ApplyList: []*GuildApply{},
		Logs:      []*GuildLog{},
	}
	return g
}

func TestGuildActorInitParsesID(t *testing.T) {
	gxyactortest.StubOwnership(t)
	g, _ := gxyactortest.Spawn(t, lib.GUILD_ACTOR_TYPE, NewGuildActor, int64(1))
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
	g := newTestGuild(t)
	m := g.getMember(200)
	if m == nil || m.Position != int32(gamecfg.GardenEGuildPosition_VICE_LEADER) {
		t.Fatal("expected vice leader")
	}
}

func TestGetMember_NotFound(t *testing.T) {
	g := newTestGuild(t)
	if m := g.getMember(999); m != nil {
		t.Fatal("expected nil")
	}
}

// ========== canApprove ==========

func TestCanApprove(t *testing.T) {
	cases := []struct {
		name   string
		roleID int64
		want   bool
	}{
		{"leader", 100, true},
		{"vice leader", 200, true},
		{"member", 300, false},
		{"non-member", 999, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := newTestGuild(t).canApprove(c.roleID); got != c.want {
				t.Fatalf("canApprove(%d) = %v, want %v", c.roleID, got, c.want)
			}
		})
	}
}

// ========== canKick ==========

func TestCanKick(t *testing.T) {
	// secondMember/secondVice:往 fixture 里补一个目标,使「同类踢同类」这类
	// 需要第二个同职位成员的格子可测(默认 fixture 只有 100/200/300 三个)。
	cases := []struct {
		name         string
		opID         int64
		targetID     int64
		secondMember bool
		secondVice   bool
		want         bool
	}{
		{"leader kick member", 100, 300, false, false, true},
		{"leader kick vice leader", 100, 200, false, false, true},
		{"vice leader kick member", 200, 300, false, false, true},
		{"vice leader cannot kick vice leader", 200, 250, false, true, false},
		{"cannot kick leader", 200, 100, false, false, false},
		{"cannot kick self", 100, 100, false, false, false},
		{"member cannot kick vice leader", 300, 200, false, false, false},
		// 回归格:少了 canKick 里的操作者职位下限,这一格会返回 true
		// (Position 3 <= Position 3),普通成员就能互相踢出公会。
		{"member cannot kick member", 300, 301, true, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newTestGuild(t)
			if c.secondVice {
				g.Data.Members = append(g.Data.Members, &GuildMember{RoleID: 250, Position: int32(gamecfg.GardenEGuildPosition_VICE_LEADER)})
			}
			if c.secondMember {
				g.Data.Members = append(g.Data.Members, &GuildMember{RoleID: 301, Position: int32(gamecfg.GardenEGuildPosition_MEMBER)})
			}
			if got := g.canKick(c.opID, c.targetID); got != c.want {
				t.Fatalf("canKick(%d, %d) = %v, want %v", c.opID, c.targetID, got, c.want)
			}
		})
	}
}

// ========== getPendingApplies ==========

func TestGetPendingApplies(t *testing.T) {
	g := newTestGuild(t)
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
	g := newTestGuild(t)
	if id := g.nextApplyID(); id != 1 {
		t.Fatalf("expected 1, got %d", id)
	}
}

func TestNextApplyID_Existing(t *testing.T) {
	g := newTestGuild(t)
	g.Data.ApplyList = []*GuildApply{{ID: 5}, {ID: 12}, {ID: 3}}
	if id := g.nextApplyID(); id != 13 {
		t.Fatalf("expected 13, got %d", id)
	}
}

// ========== onDayRefresh ==========

func TestOnDayRefresh_ClearsExpired(t *testing.T) {
	g := newTestGuild(t)
	now := time.Now()
	g.Data.ApplyList = []*GuildApply{
		{ID: 1, Status: 0, ExpireAt: now.Add(-1 * time.Hour)},
		{ID: 2, Status: 0, ExpireAt: now.Add(1 * time.Hour)},
		{ID: 3, Status: 1, ExpireAt: now.Add(-1 * time.Hour)},
		{ID: 4, Status: 0, ExpireAt: now.Add(24 * time.Hour)},
	}
	g.onDayRefresh(context.Background())
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
	g := newTestGuild(t)
	g.Data.ApplyList = []*GuildApply{
		{ID: 1, Status: 0, ExpireAt: time.Now().Add(1 * time.Hour)},
		{ID: 2, Status: 1, ExpireAt: time.Now().Add(-1 * time.Hour)},
	}
	g.onDayRefresh(context.Background())
	if len(g.Data.ApplyList) != 2 {
		t.Fatalf("expected 2, got %d", len(g.Data.ApplyList))
	}
}

// ========== buildLogList ==========

func TestBuildLogList(t *testing.T) {
	g := newTestGuild(t)
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
	g := newTestGuild(t)
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
	g := newTestGuild(t)
	g.Data.Logs = []*GuildLog{{Content: "test log", CreatedAt: time.Now()}}
	rsp, err := g.GuildLogs(context.Background(), &pb.ReqGuildLogs{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(rsp.Logs))
	}
}
