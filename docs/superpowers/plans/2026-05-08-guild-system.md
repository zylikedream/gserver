# 公会系统实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**目标：** 实现公会系统首版，包括公会创建、加入、审批、成员管理、公会聊天
**架构：** Actor（protoactor-go）模式，HTTP 仅用于 create 和 search，其余操作走 actor 消息
**Tech Stack:** Go 1.25, protoactor-go, GoFrame v2, GORM, PostgreSQL, Redis

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

// 申请加入（需审核）(29007)
message ReqApplyGuild {
    option (msg_id) = 29007;
    int64 guild_id = 1;
}
message RspApplyGuild {
    option (msg_id) = 29008;
}

// 直接加入（无需审核）(29009)
message ReqJoinGuild {
    option (msg_id) = 29009;
    int64 guild_id = 1;
}
message RspJoinGuild {
    option (msg_id) = 29010;
}

// 申请列表 (29011)
message ReqApplyList {
    option (msg_id) = 29011;
}
message RspApplyList {
    option (msg_id) = 29012;
    repeated PGuildApply applies = 1;
}

// 审批加入 (29013)
message ReqApproveApply {
    option (msg_id) = 29013;
    int64 guild_id = 1;
    int64 apply_id = 2;
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
    PRolePublic player_info = 1;
    int32       position = 2;  // 1=会长 2=副会长 3=成员
    int64       joined_at = 3;
}

message PGuildApply {
    int64 apply_id = 1;
    int64 guild_id = 2;
    PRolePublic player_info = 3;
    int64       created_at = 4;
}

message PGuildLog {
    string content = 1;
    int64  created_at = 2;
}

// ==================== 通知 ====================

// 公会内容变更（含成员列表）(29051)
message NotifyGuildInfo {
    option (msg_id) = 29051;
}

// 公会基本信息变更 (29052)
message NotifyGuildBasic {
    option (msg_id) = 29052;
}

// 被踢出公会 (29053)
message NotifyGuildKicked {
    option (msg_id) = 29053;
    string reason = 1;
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

### 任务 2: 数据模型 (model + schema)

**文件:**
- Create: `src/apps/guild/logic/model.go`
- Create: `src/apps/guild/logic/schema.go`

- [ ] **步骤 1: 创建 model.go**

```go
package logic

import (
	"time"
)

type Guild struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Name         string    `gorm:"column:name;uniqueIndex;size:32"`
	Level        int32     `gorm:"column:level"`
	Icon         string    `gorm:"column:icon;size:256"`
	Declaration  string    `gorm:"column:declaration;size:200"`
	Announcement string    `gorm:"column:announcement;size:500"`
	NeedApproval bool      `gorm:"column:need_approval"`
	MemberCount  int32     `gorm:"column:member_count"`
	LeaderID     int64     `gorm:"column:leader_id"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
	Version      int64     `gorm:"column:version"`
}

func (Guild) TableName() string { return "guild" }

type GuildMember struct {
	GuildID  int64 `gorm:"column:guild_id;primaryKey"`
	RoleID   int64 `gorm:"column:role_id;primaryKey"`
	Position int32 `gorm:"column:position"` // 1=会长 2=副会长 3=成员
	JoinedAt int64 `gorm:"column:joined_at"`
}

func (GuildMember) TableName() string { return "guild_member" }

type GuildApply struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	GuildID   int64     `gorm:"column:guild_id;index"`
	RoleID    int64     `gorm:"column:role_id"`
	Status    int32     `gorm:"column:status"` // 0=待处理 1=同意 2=拒绝
	CreatedAt time.Time `gorm:"column:created_at"`
	ExpireAt  time.Time `gorm:"column:expire_at"`
}

func (GuildApply) TableName() string { return "guild_apply" }

type GuildLog struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	GuildID   int64     `gorm:"column:guild_id;index"`
	Content   string    `gorm:"column:content;size:500"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (GuildLog) TableName() string { return "guild_log" }

// GuildRoleState 映射 role_guild 表，供 guild actor 原子操作
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
		&GuildMember{},
		&GuildApply{},
		&GuildLog{},
	); err != nil {
		glog.Fatal(ctx, err)
	}
	glog.Info(ctx, "[schema] guild tables migrated successfully")
}
```

- [ ] **步骤 3: 提交**

```bash
git add src/apps/guild/logic/model.go src/apps/guild/logic/schema.go
git commit -m "feat(guild): add data models and schema auto-migration"
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

- [ ] **步骤 2: 创建 guild_actor.go（生命周期 + 写操作）**

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
	"gserver/gameconfig"
	"gserver/protocol/pb"
	"gserver/src/lib"

	"github.com/gogf/gf/v2/os/glog"
)

type GuildActor struct {
	gxymodule.ModuleBase
	*gxyactor.ActorBase
	GuildID int64
	Data    *GuildData
}

type GuildData struct {
	Guild   *Guild
	Members []*GuildMember
}

func NewGuildActor(guildID int64) *GuildActor {
	return &GuildActor{GuildID: guildID}
}

func (g *GuildActor) OnModStart(ctx context.Context) error {
	g.Data = &GuildData{Guild: &Guild{}}
	if err := gxypgx.DB().First(g.Data.Guild, g.GuildID).Error; err != nil {
		return err
	}
	gxypgx.DB().Where("guild_id = ?", g.GuildID).Find(&g.Data.Members)

	g.Timer().AddTick(ctx, &gxytimer.Tick{Name: "guild_save", Interval: 600 * time.Second}, g.TickSave)
	g.Timer().AddCron(ctx, gxytimer.DayRefresh, g.onDayRefresh)

	glog.Infof(ctx, "guild actor started, guildID=%d, members=%d", g.GuildID, len(g.Data.Members))
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
	gxypgx.DB().Save(g.Data.Guild)
}

func (g *GuildActor) onDayRefresh(ctx context.Context, _ gxytimer.TimerActiveInfo) {
	// 清理过期申请
	gxypgx.DB().Where("guild_id = ? AND status = 0 AND expire_at < NOW()", g.GuildID).Delete(&GuildApply{})
}

// ===== 通知 =====

func (g *GuildActor) notifyPlayer(ctx context.Context, roleID int64, msg any) {
	pid, err := lib.GetRoleActor(roleID, false)
	if err != nil || pid == nil {
		return
	}
	g.Send(pid, msg)
}

func (g *GuildActor) notifyMembers(ctx context.Context, msg any, exclude ...int64) {
	excludeSet := make(map[int64]struct{}, len(exclude))
	for _, id := range exclude {
		excludeSet[id] = struct{}{}
	}
	for _, m := range g.Data.Members {
		if _, ok := excludeSet[m.RoleID]; ok {
			continue
		}
		pid, err := lib.GetRoleActor(m.RoleID, false)
		if err != nil || pid == nil {
			continue
		}
		g.Send(pid, msg)
	}
}

// ===== 日志 =====

func (g *GuildActor) addLog(ctx context.Context, content string) {
	gxypgx.DB().Create(&GuildLog{GuildID: g.GuildID, Content: content})
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
	return m != nil && m.Position <= 2 // 会长(1) 或副会长(2)
}
```

- [ ] **步骤 3: 提交**

```bash
git add src/apps/guild/logic/guild_actor.go src/lib/actor.go
git commit -m "feat(guild): add guild actor lifecycle and actor registration"
```

---

### 任务 4: Guild Actor 写操作

**文件:**
- Modify: `src/apps/guild/logic/guild_actor.go`（追加写操作）

- [ ] **步骤 1: 追加写操作到 guild_actor.go**

```go
// 追加在 guild_actor.go 末尾

const (
	PositionLeader      = 1
	PositionViceLeader  = 2
	PositionMember      = 3
)

var (
	ErrPlayerAlreadyInGuild = errors.New("该玩家已加入公会")
	ErrGuildFull            = errors.New("公会人数已满")
	ErrPermissionDenied     = errors.New("权限不足")
	ErrGuildNotFound        = errors.New("公会不存在")
	ErrCannotKickLeader     = errors.New("不能踢出会长")
	ErrCannotKickViceLeader = errors.New("不能操作同级副会长")
	ErrCannotTransferToSelf = errors.New("不能转让给自己")
	ErrGuildHasMembers      = errors.New("公会还有其他成员，请先转让会长")
	ErrApplyExpired         = errors.New("申请已过期")
	ErrPositionLimitReached = errors.New("副会长数量已达上限")
)

// ActorCreateGuild — 由 HTTP create handler 调用（guild actor 刚激活，Data 已从 DB 加载）
func (g *GuildActor) ActorCreateGuild(ctx context.Context, msg *pb.ActorCreateGuild) error {
	// Data 已由 OnModStart 从 DB 加载（HTTP handler 已写入数据）
	g.addLog(ctx, "公会创建成功")
	return nil
}

// ApplyGuild — 申请加入（需审核）
func (g *GuildActor) ApplyGuild(ctx context.Context, req *pb.ReqApplyGuild) (*pb.RspApplyGuild, error) {
	var state GuildRoleState
	err := gxypgx.DB().Where("role_id = ?", req.RoleID).First(&state).Error
	if err == nil && state.GuildID > 0 {
		return nil, ErrPlayerAlreadyInGuild
	}

	cfg := gameconfig.GameConfig().TbGuildConfig.Get(1)
	if cfg == nil {
		return nil, errors.New("公会配置未找到")
	}
	gxypgx.DB().Create(&GuildApply{
		GuildID: g.GuildID, RoleID: req.RoleID,
		Status: 0, ExpireAt: time.Now().Add(time.Duration(cfg.ApplyExpireHours) * time.Hour),
	})
	return &pb.RspApplyGuild{}, nil
}

// JoinGuild — 直接加入（无需审核）
func (g *GuildActor) JoinGuild(ctx context.Context, req *pb.ReqJoinGuild) (*pb.RspJoinGuild, error) {
	levelCfg := gameconfig.GameConfig().TbGuildLevel.Get(g.Data.Guild.Level)
	if levelCfg == nil {
		return nil, errors.New("公会等级配置未找到")
	}
	if len(g.Data.Members) >= int(levelCfg.MemberLimit) {
		return nil, ErrGuildFull
	}

	result := gxypgx.DB().Model(&GuildRoleState{}).
		Where("role_id = ? AND guild_id = 0", req.RoleID).
		Update("guild_id", g.GuildID)
	if result.RowsAffected == 0 {
		return nil, ErrPlayerAlreadyInGuild
	}

	g.Data.Members = append(g.Data.Members, &GuildMember{
		GuildID: g.GuildID, RoleID: req.RoleID,
		Position: PositionMember, JoinedAt: time.Now().Unix(),
	})
	gxypgx.DB().Create(&GuildMember{GuildID: g.GuildID, RoleID: req.RoleID, Position: PositionMember})
	g.Data.Guild.MemberCount = int32(len(g.Data.Members))
	gxypgx.DB().Save(g.Data.Guild)

	g.notifyPlayer(ctx, req.RoleID, &pb.NotifyGuildInfo{})
	g.notifyMembers(ctx, &pb.NotifyGuildInfo{}, req.RoleID)
	g.addLog(ctx, fmt.Sprintf("玩家 %d 加入公会", req.RoleID))
	return &pb.RspJoinGuild{}, nil
}

// 获取申请列表（走 actor 缓存中的 member 信息做权限判断）
func (g *GuildActor) GetApplyList(ctx context.Context, req *pb.ReqApplyList) (*pb.RspApplyList, error) {
	if !g.canApprove(g.getOperatorID(ctx)) {
		return nil, ErrPermissionDenied
	}
	var applies []GuildApply
	gxypgx.DB().Where("guild_id = ? AND status = 0", g.GuildID).Find(&applies)
	// 转换为 pb
	result := make([]*pb.PGuildApply, 0, len(applies))
	for _, a := range applies {
		result = append(result, &pb.PGuildApply{
			ApplyId: a.ID, GuildId: a.GuildID,
			CreatedAt: a.CreatedAt.Unix(),
		})
	}
	return &pb.RspApplyList{Applies: result}, nil
}
```

- [ ] **步骤 2: 提交**

```bash
git add src/apps/guild/logic/guild_actor.go
git commit -m "feat(guild): add guild actor write operations"
```

---

### 任务 5: HTTP Handler（create + search）

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

	// 写入公会记录
	guild := &Guild{
		Name: req.Name, Level: 1, LeaderID: req.LeaderID,
		Declaration: req.Declaration, Icon: req.Icon,
		NeedApproval: req.NeedApproval, MemberCount: 1,
	}
	if err := gxypgx.DB().Create(guild).Error; err != nil {
		return nil, gxyhttp.NewErrCode(1, fmt.Sprintf("创建公会失败: %v", err))
	}

	// 写入成员
	gxypgx.DB().Create(&GuildMember{
		GuildID: guild.ID, RoleID: req.LeaderID,
		Position: PositionLeader, JoinedAt: time.Now().Unix(),
	})

	// 更新 role_guild
	gxypgx.DB().Exec(
		"INSERT INTO role_guild (role_id, guild_id) VALUES (?, ?) ON CONFLICT (role_id) DO UPDATE SET guild_id = ?",
		req.LeaderID, guild.ID, guild.ID,
	)

	// 激活 guild actor（OnModStart 从 DB 加载）
	lib.GetGuildActor(guild.ID)

	// 写日志
	gxypgx.DB().Create(&GuildLog{GuildID: guild.ID, Content: "公会创建成功"})

	return map[string]int64{"guild_id": guild.ID}, nil
}

// ===== 搜索公会 =====

type SearchGuildReq struct {
	g.Meta  `path:"/search"`
	Keyword string `p:"keyword"` // 公会名称或 ID
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
		cfg := gameconfig.GameConfig().TbGuildLevel.Get(1) // 首版默认 Lv1
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
git commit -m "feat(guild): add HTTP create and search handlers"
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
	guildCfg := gameconfig.GameConfig().TbGuildConfig.Get(1)
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

	// 先扣消耗
	if err := r.Role.GetBag().SaveGoods(ctx, nil, guildCfg.CreateCost, "create_guild"); err != nil {
		return nil, err
	}

	// HTTP 创建
	guildID, err := callGuildCreate(ctx, r.RoleID, req.Name, req.Declaration, req.Icon, req.NeedApproval)
	if err != nil {
		// 创建失败退款
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

// 以下操作通过激活 guild actor 发送请求
func (r *RoleGuild) withGuildActor(ctx context.Context, req any) (any, error) {
	pid, err := lib.GetGuildActor(r.GuildID)
	if err != nil {
		return nil, fmt.Errorf("获取公会 actor 失败: %w", err)
	}
	return gxyactor.Request(pid, req)
}

func (r *RoleGuild) ReqApplyGuild(ctx context.Context, req *pb.ReqApplyGuild) (*pb.RspApplyGuild, error) {
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspApplyGuild), nil
}

func (r *RoleGuild) ReqJoinGuild(ctx context.Context, req *pb.ReqJoinGuild) (*pb.RspJoinGuild, error) {
	rsp, err := r.withGuildActor(ctx, &pb.ReqJoinGuild{RoleId: r.RoleID, GuildId: req.GuildId})
	if err != nil {
		return nil, err
	}
	r.GuildID = req.GuildId
	chat.RegisterRoleGuildChat(r.RoleID, r.GuildID, r.Role.Self())
	return rsp.(*pb.RspJoinGuild), nil
}

func (r *RoleGuild) ReqGuildInfo(ctx context.Context, req *pb.ReqGuildInfo) (*pb.RspGuildInfo, error) {
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspGuildInfo), nil
}

func (r *RoleGuild) ReqApplyList(ctx context.Context, req *pb.ReqApplyList) (*pb.RspApplyList, error) {
	rsp, err := r.withGuildActor(ctx, req)
	if err != nil {
		return nil, err
	}
	return rsp.(*pb.RspApplyList), nil
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
    case *pb.NotifyGuildInfo, *pb.NotifyGuildBasic, *pb.NotifyGuildKicked:
        r.SendClient(ctx, m.(proto.Message))
        return nil
    }
    ...
}
```

- [ ] **步骤 3: 验证编译**

```bash
cd /home/zyr/workspace/gserver && go build ./...
```

- [ ] **步骤 4: 提交**

```bash
git add src/apps/role/internal/logic/role_guild.go src/apps/role/internal/logic/role_main.go
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
