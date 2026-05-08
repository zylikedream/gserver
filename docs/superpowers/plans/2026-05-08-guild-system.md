# 公会系统实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**目标：** 实现公会系统首版，包括公会创建、加入、审批、成员管理、公会聊天
**架构：** Actor（protoactor-go）模式 + JSONB 单表，HTTP 仅用于 create 和 search，其余操作走 actor 消息
**Tech Stack:** Go 1.25, protoactor-go, GoFrame v2, GORM, PostgreSQL (JSONB), Redis

---

### 任务 1: Protobuf 定义 + 生成

**文件:**
- Create: `protocol/client/guild.proto`
- Modify: `protocol/server/gactor.proto`（如有需要）
- 生成: `make pb`

- [ ] **步骤 1: 创建 guild.proto**

```protobuf
// ID: 29001~29099
syntax = "proto3";
option go_package="./pb;pb";
package galaxy.protocol;

import "msg_options.proto";
import "role.proto";

// ==================== Req / Rsp ====================

// 创建公会 (29001)
message ReqCreateGuild {
    option (msg_id) = 29001;
    string name = 1;
    string declaration = 2;
    string icon = 3;
    bool   need_approval = 4;
}
message RspCreateGuild {
    option (msg_id) = 29002;
    int64 guild_id = 1;
}

// 搜索公会 (29003)
message ReqSearchGuild {
    option (msg_id) = 29003;
    string keyword = 1;
}
message RspSearchGuild {
    option (msg_id) = 29004;
    repeated PGuildBasic guilds = 1;
}

// 公会大厅信息 (29005)
message ReqGuildInfo {
    option (msg_id) = 29005;
}
message RspGuildInfo {
    option (msg_id) = 29006;
    PGuildBasic guild = 1;
    repeated PGuildMember members = 2;
    repeated PGuildLog logs = 3;
}

// 申请加入公会 (29007)
message ReqApplyGuild {
    option (msg_id) = 29007;
    int64 guild_id = 1;
}
message RspApplyGuild {
    option (msg_id) = 29008;
}

// 申请列表 (29011)
message ReqGuildApplyList {
    option (msg_id) = 29011;
}
message RspGuildApplyList {
    option (msg_id) = 29012;
    repeated PGuildApply applies = 1;
}

// 审批加入 (29013)
message ReqApproveApply {
    option (msg_id) = 29013;
    int64 guild_id = 1;
    repeated int64 apply_ids = 2;  // 支持批量审批
    bool  approve = 3;  // true=同意 false=拒绝
}
message RspApproveApply {
    option (msg_id) = 29014;
}

// 踢出成员 (29015)
message ReqKickMember {
    option (msg_id) = 29015;
    int64 target_id = 1;
    string reason = 2;
}
message RspKickMember {
    option (msg_id) = 29016;
}

// 任命/取消副会长 (29017)
message ReqSetViceLeader {
    option (msg_id) = 29017;
    int64 target_id = 1;
    bool  set = 2;  // true=任命 false=取消
}
message RspSetViceLeader {
    option (msg_id) = 29018;
}

// 转让会长 (29019)
message ReqTransferLeader {
    option (msg_id) = 29019;
    int64 target_id = 1;
}
message RspTransferLeader {
    option (msg_id) = 29020;
}

// 修改公会信息 (29021)
message ReqUpdateGuildInfo {
    option (msg_id) = 29021;
    string declaration = 1;
    string announcement = 2;
    bool   need_approval = 3;
}
message RspUpdateGuildInfo {
    option (msg_id) = 29022;
}

// 退出公会 (29023)
message ReqLeaveGuild {
    option (msg_id) = 29023;
}
message RspLeaveGuild {
    option (msg_id) = 29024;
}

// 解散公会 (29025)
message ReqDisbandGuild {
    option (msg_id) = 29025;
}
message RspDisbandGuild {
    option (msg_id) = 29026;
}

// ==================== 数据结构 ====================

message PGuildBasic {
    int64  id = 1;
    string name = 2;
    int32  level = 3;
    string icon = 4;
    string declaration = 5;
    string announcement = 6;
    bool   need_approval = 7;
    int32  member_count = 8;
    int32  member_limit = 9;
    int64  leader_id = 10;
    int64  created_at = 11;
}

message PGuildMember {
    PRolePublic player_info = 1;  // 由 guild actor 动态填充（GetRolePublic）
    int32       position = 2;     // 1=会长 2=副会长 3=成员
    int64       joined_at = 3;
}

message PGuildApply {
    int64 apply_id = 1;
    int64 guild_id = 2;
    PRolePublic player_info = 3;  // 由 guild actor 动态填充
    int64       created_at = 4;
}

message PGuildLog {
    string content = 1;
    int64  created_at = 2;
}

// ==================== 通知（直接携带数据） ====================

// 公会内容变更（含成员列表）(29051)
message NotifyGuildInfo {
    option (msg_id) = 29051;
    PGuildBasic        guild   = 1;
    PGuildMember       self    = 2;              // 自己的成员信息
    repeated PGuildMember members = 3;
}

// 公会基本信息变更 (29052)
message NotifyGuildBasic {
    option (msg_id) = 29052;
    PGuildBasic guild = 1;
}

// 被踢出公会 (29053)
message NotifyGuildKicked {
    option (msg_id) = 29053;
    string reason = 1;
}

// 申请列表变更 (29054)
message NotifyGuildApply {
    option (msg_id) = 29054;
    repeated PGuildApply applies = 1;
}

// ==================== Actor 内部消息 ====================

// 无需 msg_id，走 gxyactor.Request 内部路由
message ActorCreateGuild {
    int64  leader_id = 1;
    string name = 2;
    string declaration = 3;
    string icon = 4;
    bool   need_approval = 5;
}
```

- [ ] **步骤 2: 生成 protobuf Go 代码**

```bash
cd /home/zyr/workspace/gserver && make pb
```

Expected: `protocol/pb/guild.pb.go` 生成成功，无错误

- [ ] **步骤 3: 提交**

```bash
git add protocol/client/guild.proto protocol/pb/guild.pb.go
git commit -m "feat(guild): add protobuf definitions for guild system"
```

---

### 任务 2: 数据模型 (model + schema) — JSONB 单表

**文件:**
- Create: `src/apps/guild/logic/model.go`
- Create: `src/apps/guild/logic/schema.go`

- [ ] **步骤 1: 创建 model.go**

所有数据存 guild 单表，Members/ApplyList/Logs 分别存 JSONB 列：

```go
package logic

import (
	"time"
)

// Guild 公会主表，Members/ApplyList/Logs 存储为 JSONB 列
type Guild struct {
	ID           int64         `gorm:"column:id;primaryKey;autoIncrement"`
	Name         string        `gorm:"column:name;uniqueIndex;size:32"`
	Level        int32         `gorm:"column:level"`
	Icon         string        `gorm:"column:icon;size:256"`
	Declaration  string        `gorm:"column:declaration;size:200"`
	Announcement string        `gorm:"column:announcement;size:500"`
	NeedApproval bool          `gorm:"column:need_approval"`
	MemberCount  int32         `gorm:"column:member_count"`
	LeaderID     int64         `gorm:"column:leader_id"`
	Members      []GuildMember `gorm:"type:jsonb;serializer:json"`
	ApplyList    []GuildApply  `gorm:"type:jsonb;serializer:json"`
	Logs         []GuildLog    `gorm:"type:jsonb;serializer:json"`
	CreatedAt    time.Time     `gorm:"column:created_at"`
	UpdatedAt    time.Time     `gorm:"column:updated_at"`
	Version      int64         `gorm:"column:version"`
}

func (Guild) TableName() string { return "guild" }

// GuildMember — JSONB 嵌入，不存 PRolePublic，由 GetRolePublic 动态填充
type GuildMember struct {
	RoleID   int64 `json:"role_id"`
	Position int32 `json:"position"` // 1=会长 2=副会长 3=成员
	JoinedAt int64 `json:"joined_at"`
}

// GuildApply — JSONB 嵌入
type GuildApply struct {
	ID        int64     `json:"id"`
	RoleID    int64     `json:"role_id"`
	Status    int32     `json:"status"` // 0=待处理 1=同意 2=拒绝
	CreatedAt time.Time `json:"created_at"`
	ExpireAt  time.Time `json:"expire_at"`
}

// GuildLog — JSONB 嵌入（最多 100 条）
type GuildLog struct {
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// GuildRoleState 映射 role_guild 表，供 guild actor 原子操作（独立表不走 JSONB）
type GuildRoleState struct {
	RoleID  int64 `gorm:"column:role_id;primaryKey"`
	GuildID int64 `gorm:"column:guild_id"`
}

func (GuildRoleState) TableName() string { return "role_guild" }
```

- [ ] **步骤 2: 创建 schema.go**

```go
package logic

import (
	"context"

	"gserver/core/gxypgx"

	"github.com/gogf/gf/v2/os/glog"
)

func InitGuildSchema(ctx context.Context) {
	if err := gxypgx.DB().AutoMigrate(
		&Guild{},
	); err != nil {
		glog.Fatal(ctx, err)
	}
	glog.Info(ctx, "[schema] guild table migrated successfully")
}
```

- [ ] **步骤 3: 提交**

```bash
git add src/apps/guild/logic/model.go src/apps/guild/logic/schema.go
git commit -m "feat(guild): add JSONB single-table data model and schema migration"
```

---

### 任务 3: Actor 注册 + Guild Actor 生命周期

**文件:**
- Create: `src/apps/guild/logic/guild_actor.go`
- Modify: `src/lib/actor.go`

- [ ] **步骤 1: 在 src/lib/actor.go 添加公会 actor 注册**

```go
const (
	GUILD_ACTOR_TYPE = "guild"
)

func GetGuildActor(guildID int64, spawnIfNotExist ...bool) (gxyactor.PID, error) {
	pid, err := gxyactor.ActivateActor(GUILD_ACTOR_TYPE, strconv.Itoa(int(guildID)), spawnIfNotExist...)
	if err != nil {
		return nil, err
	}
	return pid, nil
}
```

- [ ] **步骤 2: 创建 guild_actor.go（生命周期 + 通知 + 权限）**

```go
package logic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gserver/core/gxymodule"
	"gserver/core/gxyactor"
	"gserver/core/gxypgx"
	"gserver/core/gxytimer"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic"
	"gserver/src/lib"

	"github.com/gogf/gf/v2/os/glog"
)

const MaxLogCount = 100

type GuildActor struct {
	gxymodule.ModuleBase
	*gxyactor.ActorBase
	GuildID int64
	Data    *GuildData
}

type GuildData struct {
	Guild     *Guild
	Members   []*GuildMember
	ApplyList []*GuildApply   // 仅 status=0 的申请
	Logs      []*GuildLog     // 最多 MaxLogCount 条
}

func NewGuildActor(guildID int64) *GuildActor {
	return &GuildActor{GuildID: guildID}
}

func (g *GuildActor) OnModStart(ctx context.Context) error {
	g.Data = &GuildData{Guild: &Guild{}}
	if err := gxypgx.DB().First(g.Data.Guild, g.GuildID).Error; err != nil {
		return err
	}
	// 从 JSONB 列加载到内存切片
	g.Data.Members = g.Data.Guild.Members
	g.Data.ApplyList = filterPending(g.Data.Guild.ApplyList)
	g.Data.Logs = g.Data.Guild.Logs

	g.Timer().AddTick(ctx, &gxytimer.Tick{Name: "guild_save", Interval: 600 * time.Second}, g.TickSave)
	g.Timer().AddCron(ctx, gxytimer.DayRefresh, g.onDayRefresh)

	glog.Infof(ctx, "guild actor started, guildID=%d, members=%d, applies=%d",
		g.GuildID, len(g.Data.Members), len(g.Data.ApplyList))
	return nil
}

func (g *GuildActor) OnModStop(ctx context.Context) error {
	g.TickSave(ctx)
	glog.Infof(ctx, "guild actor stopped, guildID=%d", g.GuildID)
	return nil
}

func (g *GuildActor) TickSave(ctx context.Context, _ ...gxytimer.TimerActiveInfo) {
	if g.Data == nil || g.Data.Guild == nil {
		return
	}
	// 写回 JSONB 列
	g.Data.Guild.Members = g.Data.Members
	g.Data.Guild.ApplyList = g.Data.Guild.ApplyList
	g.Data.Guild.Logs = g.Data.Logs
	gxypgx.DB().Save(g.Data.Guild)
}

func (g *GuildActor) onDayRefresh(ctx context.Context, _ gxytimer.TimerActiveInfo) {
	// 清理过期申请（内存 + DB）
	now := time.Now()
	valid := make([]*GuildApply, 0, len(g.Data.ApplyList))
	for _, a := range g.Data.ApplyList {
		if a.Status == 0 && a.ExpireAt.After(now) {
			valid = append(valid, a)
		}
	}
	g.Data.ApplyList = valid
	g.Data.Guild.ApplyList = valid
}

func filterPending(applies []GuildApply) []*GuildApply {
	result := make([]*GuildApply, 0, len(applies))
	for i := range applies {
		if applies[i].Status == 0 {
			result = append(result, &applies[i])
		}
	}
	return result
}

// ===== 日志 =====

func (g *GuildActor) addLog(ctx context.Context, content string) {
	entry := &GuildLog{Content: content, CreatedAt: time.Now()}
	g.Data.Logs = append(g.Data.Logs, entry)
	if len(g.Data.Logs) > MaxLogCount {
		g.Data.Logs = g.Data.Logs[len(g.Data.Logs)-MaxLogCount:]
	}
	// 日志作为审计记录，实时落盘
	g.Data.Guild.Logs = g.Data.Logs
	gxypgx.DB().Save(g.Data.Guild)
}

// ===== 通知（携带数据） =====

func (g *GuildActor) notifyPlayer(ctx context.Context, roleID int64, msg any) {
	pid, err := lib.GetRoleActor(roleID, false)
	if err != nil || pid == nil {
		return
	}
	g.Send(pid, msg)
}

func (g *GuildActor) notifyGuildInfo(ctx context.Context, exclude ...int64) {
	msg := g.buildNotifyGuildInfo(ctx)
	excludeSet := toSet(exclude)
	for _, m := range g.Data.Members {
		if _, ok := excludeSet[m.RoleID]; ok {
			continue
		}
		// 填充 self 字段
		msg.Self = g.buildPGuildMember(ctx, m)
		pid, err := lib.GetRoleActor(m.RoleID, false)
		if err != nil || pid == nil {
			continue
		}
		g.Send(pid, msg)
	}
}

func (g *GuildActor) notifyApplyUpdate(ctx context.Context) {
	msg := g.buildNotifyGuildApply(ctx)
	for _, m := range g.Data.Members {
		if m.Position > 2 {
			continue // 只有会长(1)/副会长(2)
		}
		pid, err := lib.GetRoleActor(m.RoleID, false)
		if err != nil || pid == nil {
			continue
		}
		g.Send(pid, msg)
	}
}

func (g *GuildActor) buildNotifyGuildInfo(ctx context.Context) *pb.NotifyGuildInfo {
	guild := g.Data.Guild
	levelCfg := getLevelConfig(guild.Level)
	memberLimit := int32(30)
	if levelCfg != nil {
		memberLimit = levelCfg.MemberLimit
	}
	return &pb.NotifyGuildInfo{
		Guild: &pb.PGuildBasic{
			Id: guild.ID, Name: guild.Name, Level: guild.Level,
			Icon: guild.Icon, Declaration: guild.Declaration,
			Announcement: guild.Announcement,
			NeedApproval: guild.NeedApproval,
			MemberCount:  guild.MemberCount, MemberLimit: memberLimit,
			LeaderId: guild.LeaderID, CreatedAt: guild.CreatedAt.Unix(),
		},
		Members: g.buildMemberList(ctx),
	}
}

func (g *GuildActor) buildNotifyGuildApply(ctx context.Context) *pb.NotifyGuildApply {
	result := make([]*pb.PGuildApply, 0, len(g.Data.ApplyList))
	for _, a := range g.Data.ApplyList {
		if a.Status != 0 {
			continue
		}
		pub := logic.GetRolePublic(ctx, a.RoleID)
		result = append(result, &pb.PGuildApply{
			ApplyId: a.ID, GuildId: g.GuildID,
			PlayerInfo: pub, CreatedAt: a.CreatedAt.Unix(),
		})
	}
	return &pb.NotifyGuildApply{Applies: result}
}

func (g *GuildActor) buildMemberList(ctx context.Context) []*pb.PGuildMember {
	result := make([]*pb.PGuildMember, 0, len(g.Data.Members))
	for _, m := range g.Data.Members {
		result = append(result, g.buildPGuildMember(ctx, m))
	}
	return result
}

func (g *GuildActor) buildPGuildMember(ctx context.Context, m *GuildMember) *pb.PGuildMember {
	pub := logic.GetRolePublic(ctx, m.RoleID)
	if pub == nil {
		pub = &pb.PRolePublic{RoleId: m.RoleID}
	}
	return &pb.PGuildMember{
		PlayerInfo: pub,
		Position:   m.Position,
		JoinedAt:   m.JoinedAt,
	}
}

// ===== 权限检查 =====

func (g *GuildActor) getMember(roleID int64) *GuildMember {
	for _, m := range g.Data.Members {
		if m.RoleID == roleID {
			return m
		}
	}
	return nil
}

func (g *GuildActor) canApprove(operatorID int64) bool {
	m := g.getMember(operatorID)
	return m != nil && m.Position <= 2
}

func toSet(ids []int64) map[int64]struct{} {
	s := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		s[id] = struct{}{}
	}
	return s
}

func getLevelConfig(level int32) *gameconfig.GardenGuildLevel {
	return gameconfig.GameConfig().TbGuildLevel.Get(level)
}
```

Note: The import for `gameconfig` will be in guild_actor.go — the `getLevelConfig` helper at the bottom uses it. Also note the import pattern: `logic.GetRolePublic` is from `src/apps/role/internal/logic` (aliased as `logic` when imported in the `guild/logic` package — the Go compiler handles this via the module path. The actual import in guild_actor.go will be:

```go
"gserver/src/apps/role/internal/logic"
```

And the function is called as `rolelogic.GetRolePublic(ctx, roleID)` (with a local import alias to avoid name collision with the package name `logic`). Adjust the helper calls accordingly.

- [ ] **步骤 3: 提交**

```bash
git add src/apps/guild/logic/guild_actor.go src/lib/actor.go
git commit -m "feat(guild): add guild actor lifecycle with JSONB data loading"
```

---

### 任务 4: Guild Actor 写操作

**文件:**
- Modify: `src/apps/guild/logic/guild_actor.go`（追加写操作）

- [ ] **步骤 1: 追加写操作到 guild_actor.go**

```go
// 追加在 guild_actor.go 末尾

var (
	ErrPlayerAlreadyInGuild = errors.New("该玩家已加入公会")
	ErrGuildFull            = errors.New("公会人数已满")
	ErrPermissionDenied     = errors.New("权限不足")
	ErrGuildNotFound        = errors.New("公会不存在")
	ErrCannotKickLeader     = errors.New("不能踢出会长")
	ErrCannotKickViceLeader = errors.New("不能操作同级副会长")
	ErrCannotTransferToSelf = errors.New("不能转让给自己")
	ErrGuildHasMembers      = errors.New("公会还有其他成员，请先转让会长")
	ErrApplyExpired         = errors.New("申请已过期或不存在")
	ErrPositionLimitReached = errors.New("副会长数量已达上限")
)

// nextApplyID 生成自增 apply_id（基于现有最大 ID + 1）
func (g *GuildActor) nextApplyID() int64 {
	var maxID int64
	for _, a := range g.Data.ApplyList {
		if a.ID > maxID {
			maxID = a.ID
		}
	}
	return maxID + 1
}

// ActorCreateGuild — 由 HTTP create handler 调用（guild actor 刚激活，Data 已从 DB 加载）
func (g *GuildActor) ActorCreateGuild(ctx context.Context, msg *pb.ActorCreateGuild) error {
	g.addLog(ctx, "公会创建成功")
	return nil
}

// ---- 加入公会（内部根据 NeedApproval 分流） ----

// ApplyGuild — 申请加入公会（根据 NeedApproval 分流）
func (g *GuildActor) ApplyGuild(ctx context.Context, req *pb.ReqApplyGuild) (*pb.RspApplyGuild, error) {
	var state GuildRoleState
	err := gxypgx.DB().Where("role_id = ?", req.RoleID).First(&state).Error
	if err == nil && state.GuildID > 0 {
		return nil, ErrPlayerAlreadyInGuild
	}

	if g.Data.Guild.NeedApproval {
		return g.createApply(ctx, req)
	}
	return g.joinDirect(ctx, req)
}

// createApply — 创建申请
func (g *GuildActor) createApply(ctx context.Context, req *pb.ReqApplyGuild) (*pb.RspApplyGuild, error) {
	cfg := gameconfig.GameConfig().TbGuildConfig.Get()
	if cfg == nil {
		return nil, errors.New("公会配置未找到")
	}

	apply := &GuildApply{
		ID:        g.nextApplyID(),
		RoleID:    req.RoleID,
		Status:    0,
		CreatedAt: time.Now(),
		ExpireAt:  time.Now().Add(time.Duration(cfg.ApplyExpireHours) * time.Hour),
	}
	g.Data.ApplyList = append(g.Data.ApplyList, apply)
	g.Data.Guild.ApplyList = g.Data.ApplyList
	g.notifyApplyUpdate(ctx)
	return &pb.RspApplyGuild{}, nil
}

// addMember — 原子操作：核验成员上限 → 原子门 → 追加成员 → 通知
func (g *GuildActor) addMember(ctx context.Context, roleID int64) error {
	levelCfg := gameconfig.GameConfig().TbGuildLevel.Get(g.Data.Guild.Level)
	if levelCfg == nil {
		return errors.New("公会等级配置未找到")
	}
	if len(g.Data.Members) >= int(levelCfg.MemberLimit) {
		return ErrGuildFull
	}

	result := gxypgx.DB().Model(&GuildRoleState{}).
		Where("role_id = ? AND guild_id = 0", roleID).
		Update("guild_id", g.GuildID)
	if result.RowsAffected == 0 {
		return ErrPlayerAlreadyInGuild
	}

	member := &GuildMember{RoleID: roleID, Position: gamecfg.GardenEGuildPosition_MEMBER, JoinedAt: time.Now().Unix()}
	g.Data.Members = append(g.Data.Members, member)
	g.Data.Guild.Members = g.Data.Members
	g.Data.Guild.MemberCount = int32(len(g.Data.Members))

	g.notifyGuildInfo(ctx, roleID)
	g.addLog(ctx, fmt.Sprintf("玩家 %d 加入公会", roleID))
	return nil
}

// joinDirect — 直接加入（免审批）
func (g *GuildActor) joinDirect(ctx context.Context, req *pb.ReqApplyGuild) (*pb.RspApplyGuild, error) {
	if err := g.addMember(ctx, req.RoleID); err != nil {
		return nil, err
	}
	return &pb.RspApplyGuild{}, nil
}

// ---- 申请列表 ----

// GetGuildApplyList — 从内存返回（带 PRolePublic 信息）
func (g *GuildActor) GetGuildApplyList(ctx context.Context, req *pb.ReqGuildApplyList) (*pb.RspGuildApplyList, error) {
	if !g.canApprove(g.getOperatorID(ctx)) {
		return nil, ErrPermissionDenied
	}
	result := make([]*pb.PGuildApply, 0, len(g.Data.ApplyList))
	for _, a := range g.Data.ApplyList {
		if a.Status != 0 {
			continue
		}
		pub := rolelogic.GetRolePublic(ctx, a.RoleID)
		result = append(result, &pb.PGuildApply{
			ApplyId: a.ID, GuildId: g.GuildID,
			PlayerInfo: pub, CreatedAt: a.CreatedAt.Unix(),
		})
	}
	return &pb.RspGuildApplyList{Applies: result}, nil
}

// ---- 审批 ----

func (g *GuildActor) ApproveApply(ctx context.Context, req *pb.ReqApproveApply) error {
	if !g.canApprove(req.OperatorID) {
		return ErrPermissionDenied
	}

	// 支持批量审批
	for _, applyID := range req.ApplyIds {
		if err := g.processSingleApply(ctx, applyID, req.Approve); err != nil {
			return err
		}
	}
	return nil
}

// processSingleApply 处理单个申请审批
func (g *GuildActor) processSingleApply(ctx context.Context, applyID int64, approve bool) error {
	var apply *GuildApply
	for _, a := range g.Data.ApplyList {
		if a.ID == applyID && a.Status == 0 {
			apply = a
			break
		}
	}
	if apply == nil {
		return ErrApplyExpired
	}

	// 拒绝：仅更新状态
	if !approve {
		apply.Status = 2
		g.Data.Guild.ApplyList = g.Data.ApplyList
		g.notifyApplyUpdate(ctx)
		return nil
	}

	// 同意
	apply.Status = 1
	g.Data.Guild.ApplyList = g.Data.ApplyList
	if err := g.addMember(ctx, apply.RoleID); err != nil {
		return err
	}
	g.notifyApplyUpdate(ctx)
	return nil
}

// ---- 踢出 ----

func (g *GuildActor) KickMember(ctx context.Context, req *pb.KickMemberReq) error {
	if !g.canKick(req.OperatorID, req.TargetID) {
		return ErrPermissionDenied
	}

	g.Data.Members = removeMember(g.Data.Members, req.TargetID)
	g.Data.Guild.Members = g.Data.Members
	g.Data.Guild.MemberCount = int32(len(g.Data.Members))

	gxypgx.DB().Model(&GuildRoleState{}).
		Where("role_id = ?", req.TargetID).Update("guild_id", 0)

	g.notifyPlayer(ctx, req.TargetID, &pb.NotifyGuildKicked{Reason: req.Reason})
	g.notifyGuildInfo(ctx, req.TargetID)

	g.addLog(ctx, fmt.Sprintf("玩家 %d 被 %d 踢出公会", req.TargetID, req.OperatorID))
	return nil
}

// ---- 辅助 ----

func removeMember(members []*GuildMember, roleID int64) []*GuildMember {
	for i, m := range members {
		if m.RoleID == roleID {
			return append(members[:i], members[i+1:]...)
		}
	}
	return members
}

func (g *GuildActor) canKick(operatorID, targetID int64) bool {
	if operatorID == targetID {
		return false
	}
	op := g.getMember(operatorID)
	target := g.getMember(targetID)
	if op == nil || target == nil {
		return false
	}
	// 只有会长(1)可以踢副会长(2)，会长和副会长都可以踢成员(3)
	if target.Position == gamecfg.GardenEGuildPosition_LEADER {
		return false
	}
	if target.Position == gamecfg.GardenEGuildPosition_VICE_LEADER && op.Position != gamecfg.GardenEGuildPosition_LEADER {
		return false
	}
	return op.Position <= target.Position
}
```

Note: `rolelogic` is the import alias for `gserver/src/apps/role/internal/logic`. The actual import line should be:

```go
rolelogic "gserver/src/apps/role/internal/logic"
```

- [ ] **步骤 2: 提交**

```bash
git add src/apps/guild/logic/guild_actor.go
git commit -m "feat(guild): add guild actor write operations with in-memory apply/log"
```

---

### 任务 5: HTTP Handler（create + search）— JSONB 写入

**文件:**
- Create: `src/apps/guild/logic/handler.go`

- [ ] **步骤 1: 创建 handler.go**

```go
package logic

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"gserver/core/gxyhttp"
	"gserver/core/gxypgx"
	"gserver/gameconfig"
	"gserver/protocol/pb"
	"gserver/src/lib"

	"github.com/gogf/gf/v2/frame/g"
)

type GuildHandler struct {
	g.Meta `method:"POST"`
}

// ===== 创建公会 =====

type CreateGuildReq struct {
	g.Meta       `path:"/create"`
	LeaderID     int64  `p:"leader_id" v:"required"`
	Name         string `p:"name" v:"required"`
	Declaration  string `p:"declaration"`
	Icon         string `p:"icon"`
	NeedApproval bool   `p:"need_approval"`
}

func (h *GuildHandler) Create(ctx context.Context, req *CreateGuildReq) (any, error) {
	// 检查名称唯一性
	var count int64
	gxypgx.DB().Model(&Guild{}).Where("name = ?", req.Name).Count(&count)
	if count > 0 {
		return nil, gxyhttp.NewErrCode(1, "公会名称已存在")
	}

	// 写入公会记录（JSONB 列直接初始化 Members/Logs）
	guild := &Guild{
		Name: req.Name, Level: 1, LeaderID: req.LeaderID,
		Declaration: req.Declaration, Icon: req.Icon,
		NeedApproval: req.NeedApproval, MemberCount: 1,
		Members: []GuildMember{
			{RoleID: req.LeaderID, Position: gamecfg.GardenEGuildPosition_LEADER, JoinedAt: time.Now().Unix()},
		},
		Logs: []GuildLog{
			{Content: "公会创建成功", CreatedAt: time.Now()},
		},
	}
	if err := gxypgx.DB().Create(guild).Error; err != nil {
		return nil, gxyhttp.NewErrCode(1, fmt.Sprintf("创建公会失败: %v", err))
	}

	// 更新 role_guild
	gxypgx.DB().Exec(
		"INSERT INTO role_guild (role_id, guild_id) VALUES (?, ?) ON CONFLICT (role_id) DO UPDATE SET guild_id = ?",
		req.LeaderID, guild.ID, guild.ID,
	)

	// 激活 guild actor（OnModStart 从 DB 加载）
	lib.GetGuildActor(guild.ID)

	return map[string]int64{"guild_id": guild.ID}, nil
}

// ===== 搜索公会 =====

type SearchGuildReq struct {
	g.Meta  `path:"/search"`
	Keyword string `p:"keyword"`
}

func (h *GuildHandler) Search(ctx context.Context, req *SearchGuildReq) (any, error) {
	var guilds []Guild
	if id, err := strconv.ParseInt(req.Keyword, 10, 64); err == nil {
		gxypgx.DB().Where("id = ?", id).Find(&guilds)
	} else {
		gxypgx.DB().Where("name LIKE ?", "%"+req.Keyword+"%").Limit(20).Find(&guilds)
	}

	result := make([]*pb.PGuildBasic, 0, len(guilds))
	for _, g := range guilds {
		cfg := gameconfig.GameConfig().TbGuildLevel.Get(g.Level)
		memberLimit := int32(30)
		if cfg != nil {
			memberLimit = cfg.MemberLimit
		}
		result = append(result, &pb.PGuildBasic{
			Id: g.ID, Name: g.Name, Level: g.Level,
			Icon: g.Icon, Declaration: g.Declaration,
			NeedApproval: g.NeedApproval,
			MemberCount:  g.MemberCount, MemberLimit: memberLimit,
			LeaderId: g.LeaderID, CreatedAt: g.CreatedAt.Unix(),
		})
	}
	return result, nil
}
```

- [ ] **步骤 2: 提交**

```bash
git add src/apps/guild/logic/handler.go
git commit -m "feat(guild): add HTTP create and search handlers with JSONB init"
```

---

### 任务 6: Guild App + Service（总装）

**文件:**
- Create: `src/apps/guild/guild_app.go`
- Create: `src/apps/guild/guild_service.go`
- Modify: config TOML（添加 guild 到 app 列表）

- [ ] **步骤 1: 创建 guild_app.go**

```go
package guild

import (
	"context"

	"gserver/core/gxyapp"
	"gserver/core/gxyservice"
	"gserver/gameconfig"
	"gserver/src/apps/guild/logic"
)

type guildApp struct {
	gxyapp.App
}

func NewGuildApp() *guildApp {
	return &guildApp{}
}

func (g *guildApp) ServiceName() string {
	return "guild"
}

func (g *guildApp) OnModInit(ctx context.Context) error {
	g.AddModule(ctx, gameconfig.NewGameConfig())
	logic.InitGuildSchema(ctx)
	gxyservice.ServiceApp().LoadService(ctx, NewGuildService())
	return nil
}
```

- [ ] **步骤 2: 创建 guild_service.go**

```go
package guild

import (
	"context"

	"gserver/core/gxyhttp"
	"gserver/core/gxyservice"
	"gserver/src/apps/guild/logic"

	"github.com/gogf/gf/v2/os/glog"
)

type guildService struct {
	gxyhttp.HttpService
}

func NewGuildService() *guildService {
	return &guildService{}
}

func (s *guildService) ServiceName() string {
	return "guild"
}

func (s *guildService) OnModStart(ctx context.Context) error {
	host := gxyservice.ServiceApp().Host
	svr := gxyhttp.HttpSystem().NewHttpServer(host)
	gxyhttp.SetHandler(svr, ctx, "guild", &logic.GuildHandler{})
	glog.Infof(ctx, "guild server starting")
	if err := svr.Start(); err != nil {
		return err
	}
	s.Svr = svr
	return nil
}

func (s *guildService) OnModStop(ctx context.Context) error {
	glog.Infof(ctx, "guild service stopping")
	return s.Svr.Shutdown()
}
```

- [ ] **步骤 3: 将 guildApp 注册到 config**

检查 `config/*.toml` 中的 app 列表，添加 `"guild"`。

```bash
grep -rn "apps\b" config/*.toml | head -5
# 确认后添加 "guild"
```

- [ ] **步骤 4: 验证编译**

```bash
cd /home/zyr/workspace/gserver && go build ./...
```

Expected: 编译通过，无错误

- [ ] **步骤 5: 提交**

```bash
git add src/apps/guild/ config/*.toml
git commit -m "feat(guild): add guild app and HTTP service"
```

---

### 任务 7: RoleGuild 子模块

**文件:**
- Create: `src/apps/role/internal/logic/role_guild.go`
- Modify: `src/apps/role/internal/logic/role_main.go`（嵌入 roleModules + HandleMessage）

- [ ] **步骤 1: 创建 role_guild.go**

```go
package logic

import (
	"context"
	"fmt"
	"net/url"

	"gserver/core/gxyactor"
	"gserver/core/gxyhttp"
	"gserver/gameconfig"
	"gserver/protocol/pb"
	"gserver/src/apps/chat"
	"gserver/src/lib"

	"github.com/gogf/gf/v2/util/gconv"
)

// ===== 持久化状态 =====

type RoleGuildState struct {
	RolePersistState
	GuildID int64 `gorm:"column:guild_id"` // 0=无公会
}

func (RoleGuildState) TableName() string { return "role_guild" }

// ===== 子模块 =====

type RoleGuild struct {
	RoleModule
	RoleGuildState
}

var _ IRoleModule = (*RoleGuild)(nil)

func (r *RoleGuild) PersistState() IPersistState { return &r.RoleGuildState }

func (r *RoleGuild) OnModInit(ctx context.Context) error { return nil }

func (r *RoleGuild) OnCreate(ctx context.Context) {}

func (r *RoleGuild) OnModStart(ctx context.Context) error {
	if r.GuildID > 0 {
		chat.RegisterRoleGuildChat(r.RoleID, r.GuildID, r.Role.Self())
	}
	return nil
}

// ===== HTTP helpers =====

func callGuildCreate(ctx context.Context, leaderID int64, name, declaration, icon string, needApproval bool) (int64, error) {
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "guild",
		fmt.Sprintf("create?leader_id=%d&name=%s&declaration=%s&icon=%s&need_approval=%t",
			leaderID, url.QueryEscape(name), url.QueryEscape(declaration), url.QueryEscape(icon), needApproval))
	if err != nil {
		return 0, err
	}
	data := struct {
		GuildID int64 `json:"guild_id"`
	}{}
	if err := gconv.Scan(rsp.Data, &data); err != nil {
		return 0, fmt.Errorf("parse guild_id: %w", err)
	}
	return data.GuildID, nil
}

func callGuildSearch(ctx context.Context, keyword string) ([]*pb.PGuildBasic, error) {
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, "guild",
		fmt.Sprintf("search?keyword=%s", url.QueryEscape(keyword)))
	if err != nil {
		return nil, err
	}
	var guilds []*pb.PGuildBasic
	if err := gconv.Scan(rsp.Data, &guilds); err != nil {
		return nil, fmt.Errorf("parse search result: %w", err)
	}
	return guilds, nil
}

// ===== Proto Handlers =====

func (r *RoleGuild) ReqCreateGuild(ctx context.Context, req *pb.ReqCreateGuild) (*pb.RspCreateGuild, error) {
	basic := r.Role.GetBasic()
	guildCfg := gameconfig.GameConfig().TbGuildConfig.Get()
	if guildCfg == nil {
		return nil, fmt.Errorf("公会配置未找到")
	}
	if basic.Level < guildCfg.UnlockLevel {
		return nil, fmt.Errorf("等级不足，需要 Lv%d", guildCfg.UnlockLevel)
	}
	if r.GuildID > 0 {
		return nil, fmt.Errorf("你已加入公会")
	}
	if !r.Role.GetBag().CheckGoods(guildCfg.CreateCost) {
		return nil, fmt.Errorf("创建公会消耗不足")
	}

	if err := r.Role.GetBag().SaveGoods(ctx, nil, guildCfg.CreateCost, "create_guild"); err != nil {
		return nil, err
	}

	guildID, err := callGuildCreate(ctx, r.RoleID, req.Name, req.Declaration, req.Icon, req.NeedApproval)
	if err != nil {
		r.Role.GetBag().SaveGoods(ctx, guildCfg.CreateCost, nil, "create_guild_refund")
		return nil, err
	}

	r.GuildID = guildID
	chat.RegisterRoleGuildChat(r.RoleID, guildID, r.Role.Self())
	return &pb.RspCreateGuild{GuildId: guildID}, nil
}

func (r *RoleGuild) ReqSearchGuild(ctx context.Context, req *pb.ReqSearchGuild) (*pb.RspSearchGuild, error) {
	guilds, err := callGuildSearch(ctx, req.Keyword)
	if err != nil {
		return nil, err
	}
	return &pb.RspSearchGuild{Guilds: guilds}, nil
}

func (r *RoleGuild) withGuildActor(ctx context.Context, req any) (any, error) {
	pid, err := lib.GetGuildActor(r.GuildID)
	if err != nil {
		return nil, fmt.Errorf("获取公会 actor 失败: %w", err)
	}
	return gxyactor.Request(pid, req)
}

func (r *RoleGuild) ReqApplyGuild(ctx context.Context, req *pb.ReqApplyGuild) (*pb.RspApplyGuild, error) {
	// 玩家还没公会，不能用 r.withGuildActor（r.GuildID == 0），直接激活目标公会 actor
	pid, err := lib.GetGuildActor(req.GuildId)
	if err != nil {
		return nil, fmt.Errorf("获取公会 actor 失败: %w", err)
	}
	rsp, err := gxyactor.Request(pid, &pb.ReqApplyGuild{RoleId: r.RoleID, GuildId: req.GuildId})
	if err != nil {
		return nil, err
	}
	// 加入成功后更新角色公会信息
	r.GuildID = req.GuildId
	chat.RegisterRoleGuildChat(r.RoleID, r.GuildID, r.Role.Self())
	return rsp.(*pb.RspApplyGuild), nil
}

func (r *RoleGuild) ReqGuildInfo(ctx context.Context, req *pb.ReqGuildInfo) (*pb.RspGuildInfo, error) {
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspGuildInfo), nil
}

func (r *RoleGuild) ReqGuildApplyList(ctx context.Context, req *pb.ReqGuildApplyList) (*pb.RspGuildApplyList, error) {
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspGuildApplyList), nil
}

func (r *RoleGuild) ReqApproveApply(ctx context.Context, req *pb.ReqApproveApply) (*pb.RspApproveApply, error) {
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspApproveApply), nil
}

func (r *RoleGuild) ReqKickMember(ctx context.Context, req *pb.ReqKickMember) (*pb.RspKickMember, error) {
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspKickMember), nil
}

func (r *RoleGuild) ReqSetViceLeader(ctx context.Context, req *pb.ReqSetViceLeader) (*pb.RspSetViceLeader, error) {
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspSetViceLeader), nil
}

func (r *RoleGuild) ReqTransferLeader(ctx context.Context, req *pb.ReqTransferLeader) (*pb.RspTransferLeader, error) {
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspTransferLeader), nil
}

func (r *RoleGuild) ReqUpdateGuildInfo(ctx context.Context, req *pb.ReqUpdateGuildInfo) (*pb.RspUpdateGuildInfo, error) {
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspUpdateGuildInfo), nil
}

func (r *RoleGuild) ReqLeaveGuild(ctx context.Context, req *pb.ReqLeaveGuild) (*pb.RspLeaveGuild, error) {
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	r.GuildID = 0
	chat.UnregisterRoleGuildChat(r.RoleID)
	return rsp.(*pb.RspLeaveGuild), nil
}

func (r *RoleGuild) ReqDisbandGuild(ctx context.Context, req *pb.ReqDisbandGuild) (*pb.RspDisbandGuild, error) {
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	r.GuildID = 0
	chat.UnregisterRoleGuildChat(r.RoleID)
	return rsp.(*pb.RspDisbandGuild), nil
}
```

- [ ] **步骤 2: 嵌入 roleModules + HandleMessage**

在 `role_main.go` 的 `roleModules` 中添加 `Guild *RoleGuild`，在 `HandleMessage` 中添加通知路由：

```go
// role_modules 结构体追加 Guild
type roleModules struct {
    // ... 现有字段 ...
    Guild         *RoleGuild
}

// HandleMessage 添加 guild 通知
func (r *RoleMain) HandleMessage(ctx context.Context, msg any) error {
    switch m := msg.(type) {
    case *pb.NotifyWorldChat, *pb.NotifySystemChat, *pb.NotifyPrivateChat:
        r.SendClient(ctx, m.(proto.Message))
        return nil
    case *pb.NotifyGuildInfo, *pb.NotifyGuildBasic, *pb.NotifyGuildKicked, *pb.NotifyGuildApply:
        r.SendClient(ctx, m.(proto.Message))
        return nil
    }
    ...
}
```

还需要在 `role_schema.go` 的 `InitRoleSchema` 中添加 `&RoleGuildState{}`：

```go
func InitRoleSchema(ctx context.Context) {
    if err := gxypgx.DB().AutoMigrate(
        &RoleAccount{},
        ...
        &RoleChatState{},
        &RoleGuildState{},  // ← 新增
    ); err != nil {
        ...
    }
}
```

- [ ] **步骤 3: 验证编译**

```bash
cd /home/zyr/workspace/gserver && go build ./...
```

- [ ] **步骤 4: 提交**

```bash
git add src/apps/role/internal/logic/role_guild.go src/apps/role/internal/logic/role_main.go src/apps/role/internal/logic/role_schema.go
git commit -m "feat(guild): add RoleGuild sub-module with proto handlers"
```

---

### 任务 8: 公会聊天集成

**文件:**
- Modify: `src/apps/chat/sidecar.go`
- Modify: `src/apps/chat/redis.go`
- Modify: `src/apps/chat/handler.go`
- Add: `src/apps/chat/role_guild_chat.go`（或追加到现有文件）

- [ ] **步骤 1: 在 chat 包中新增公会频道注册**

```go
// chat/sidecar.go 追加

var localGuildRoles sync.Map // roleID → *guildRoleEntry

type guildRoleEntry struct {
	guildID int64
	pid     gxyactor.PID
}

func RegisterRoleGuildChat(roleID int64, guildID int64, pid gxyactor.PID) {
	localGuildRoles.Store(roleID, &guildRoleEntry{guildID: guildID, pid: pid})
}

func UnregisterRoleGuildChat(roleID int64) {
	localGuildRoles.Delete(roleID)
}
```

- [ ] **步骤 2: sidecar 增加公会频道路由**

在 `handleSidecarMsg` 中追加 `case strings.HasPrefix(channel, "chat:pub:guild:")`:

```go
} else if strings.HasPrefix(channel, "chat:pub:guild:") {
	parts := strings.SplitN(channel, ":", 4)
	if len(parts) < 4 {
		return
	}
	guildID, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return
	}
	chatMsg, err := jsonToMsg(msg.Payload)
	if err != nil {
		return
	}
	notify := &pb.NotifyGuildChat{Message: chatMsg}
	localGuildRoles.Range(func(key, value any) bool {
		entry := value.(*guildRoleEntry)
		if entry.guildID == guildID {
			gxyactor.LocalSend(entry.pid, notify)
		}
		return true
	})
```

- [ ] **步骤 3: 在 chat/redis.go 添加公会消息函数**

```go
// ===== 公会频道 =====

func StoreGuildMsgData(ctx context.Context, data string, guildID int64) error {
	cfg := GetConfig()
	key := fmt.Sprintf("chat:msg:guild:%d", guildID)
	pipe := gxyredis.Redis().Pipeline()
	pipe.LPush(ctx, key, data)
	pipe.LTrim(ctx, key, 0, int64(cfg.WorldMsgKeep-1))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("chat store guild msg: %w", err)
	}
	return nil
}

func PublishGuildChatData(ctx context.Context, data string, guildID int64) error {
	channel := fmt.Sprintf("chat:pub:guild:%d", guildID)
	return gxyredis.Redis().Publish(ctx, channel, data).Err()
}

func GetGuildHistory(ctx context.Context, guildID int64, count int) ([]*pb.PChatMsg, error) {
	key := fmt.Sprintf("chat:msg:guild:%d", guildID)
	results, err := gxyredis.Redis().LRange(ctx, key, 0, int64(count-1)).Result()
	if err != nil {
		return nil, err
	}
	return parseMsgList(results)
}
```

- [ ] **步骤 4: 在 chat/handler.go 添加公会聊天 API**

```go
// ===== 公会频道 =====

type SendGuildChatReq struct {
	g.Meta  `path:"/send_guild"`
	Sender  string `p:"sender" v:"required"`
	GuildID int64  `p:"guild_id" v:"required"`
	Content string `p:"content" v:"required"`
}

func (h *ChatHandler) SendGuildChat(ctx context.Context, req *SendGuildChatReq) (any, error) {
	cfg := GetConfig()
	trimmed := strings.TrimSpace(req.Content)
	if trimmed == "" {
		return nil, gxyhttp.NewErrCode(1, "消息不能为空")
	}
	if len([]rune(trimmed)) > cfg.MsgMaxLength {
		return nil, gxyhttp.NewErrCode(1, "消息超过字数限制")
	}
	var sender *pb.PRolePublic
	if err := json.Unmarshal([]byte(req.Sender), &sender); err != nil {
		return nil, gxyhttp.NewErrCode(1, "parse sender error")
	}
	msg := &chatMsgJSON{Sender: sender, Content: trimmed, Timestamp: time.Now().Unix()}
	data := msgToJSON(msg)
	if err := StoreGuildMsgData(ctx, data, req.GuildID); err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	if err := PublishGuildChatData(ctx, data, req.GuildID); err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	return nil, nil
}

type GuildHistoryReq struct {
	g.Meta  `path:"/guild_history"`
	GuildID int64 `p:"guild_id" v:"required"`
	Count   int   `p:"count"`
}

func (h *ChatHandler) GuildHistory(ctx context.Context, req *GuildHistoryReq) (any, error) {
	cfg := GetConfig()
	count := req.Count
	if count <= 0 {
		count = cfg.WorldMsgKeep
	}
	msgs, err := GetGuildHistory(ctx, req.GuildID, count)
	if err != nil {
		return nil, gxyhttp.NewErrCode(1, err.Error())
	}
	return msgs, nil
}
```

- [ ] **步骤 5: 提交**

```bash
git add src/apps/chat/
git commit -m "feat(guild): add guild chat channel with Redis pub/sub"
```

---

### 任务 9: 完整验证

- [ ] **步骤 1: 全量编译**

```bash
cd /home/zyr/workspace/gserver && go build ./...
```

Expected: 编译通过，无错误

- [ ] **步骤 2: 检查模块注册**

确认 `src/apps/role/internal/logic/role_main.go` 的 `initRoleModules` 能正确检测到 `Guild` 字段（通过反射），`RoleGuild` 实现了 `IRoleModule` 接口。

- [ ] **步骤 3: 提交最终改动**

```bash
git add -A
git commit -m "feat(guild): complete guild system phase 1 implementation"
```
