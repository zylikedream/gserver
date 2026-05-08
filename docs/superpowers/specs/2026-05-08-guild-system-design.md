# 公会系统设计文档

> 日期：2026-05-08
> 范围：首版公会系统
> 架构模式：Actor（protoactor-go）+ HTTP 只读接口 + Chat 模块集成

---

## 1. 整体架构

```
┌─────────────────────────────────────────────────────┐
│ src/apps/guild/                                     │
│                                                     │
│  guild_app.go    ─── App 注册，AutoMigrate          │
│                                                     │
│  guild_actor.go  ─── 公会 Actor（核心）             │
│   ├── 数据：GuildInfo + Members 内存缓存            │
│   ├── 生命周期：OnModStart 加载，TickSave 持久化    │
│   ├── 定时器：DayRefresh 自动转让/清理              │
│   └── 写操作：创建/审批/踢人/转让/修改信息/解散    │
│                                                     │
│  handler.go      ─── HTTP 只读操作                  │
│   └── 搜索公会、查看公会基本信息                    │
│                                                     │
│  model.go        ─── GORM 模型                      │
│  schema.go       ─── AutoMigrate                    │
│  guild.go        ─── 核心业务逻辑函数               │
│                                                     │
├─────────────────────────────────────────────────────┤
│ src/apps/role/internal/logic/role_guild.go           │
│  RoleGuild 子模块                                    │
│  ├── 持久化：RoleGuildState（role_id → guild_id）   │
│  ├── 读操作 → HTTP guild service                    │
│  └── 写操作 → 激活 guild actor → actor 消息        │
│                                                     │
├─────────────────────────────────────────────────────┤
│ src/apps/chat/                                       │
│  新增公会频道：chat:pub:guild:{guild_id}             │
│  sidecar 新增路由，按 guild_id 推送给本节点成员     │
└─────────────────────────────────────────────────────┘
```

### 分工原则

| 操作类型 | 实现方式 | 说明 |
|---------|---------|------|
| 只读操作（搜索、查看基本信息、申请列表、日志） | HTTP | 不需要激活 actor，直接查 DB |
| 创建公会 | HTTP | 先扣消耗 → HTTP 创建（写入 DB + 激活 actor） |
| 申请加入（需审核） | HTTP | 只写 guild_apply 表，原子门在审批时做 |
| 加入公会（无需审核） | Actor 消息 | 直接改成员列表 + role_guild，atomic gate |
| 写操作（审批、踢人、任命、转让、修改信息、退出、解散） | Actor 消息 | 需要操作公会状态，mailbox 串行化保护 |
| 定时任务（自动转让、过期清理） | Actor 内部 Timer | 在 guild actor 的 DayRefresh 中执行 |
| 聊天推送 | Redis pub/sub + sidecar | 由 chat 模块统一处理 |

---

## 2. 数据模型

### 2.1 guild 表 — 公会主表

```go
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
    Version      int64     `gorm:"column:version"` // 乐观锁
}
```

### 2.2 guild_member 表 — 成员

```go
type GuildMember struct {
    GuildID  int64 `gorm:"column:guild_id;primaryKey"`
    RoleID   int64 `gorm:"column:role_id;primaryKey"`
    Position int32 `gorm:"column:position"` // 1=会长 2=副会长 3=成员
    JoinedAt int64 `gorm:"column:joined_at"`
}
```

### 2.3 guild_apply 表 — 申请

```go
type GuildApply struct {
    ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
    GuildID   int64     `gorm:"column:guild_id;index"`
    RoleID    int64     `gorm:"column:role_id"`
    Status    int32     `gorm:"column:status"` // 0=待处理 1=同意 2=拒绝
    CreatedAt time.Time `gorm:"column:created_at"`
    ExpireAt  time.Time `gorm:"column:expire_at"`
}
```

### 2.4 guild_log 表 — 公会日志

```go
type GuildLog struct {
    ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
    GuildID   int64     `gorm:"column:guild_id;index"`
    Content   string    `gorm:"column:content;size:500"`
    CreatedAt time.Time `gorm:"column:created_at"`
}
```

日志保留最近 N 条（由配置决定），按 `created_at DESC` 查询。

### 2.5 role_guild 表 — RoleGuild 子模块状态

```go
type RoleGuildState struct {
    RoleID  int64 `gorm:"column:role_id;primaryKey"`
    GuildID int64 `gorm:"column:guild_id"` // 0 = 无公会
}
```

这是 role 侧的唯一公会数据，只存「我在哪个公会」。

### 2.6 完整关系图

```
guild (1) ──→ guild_member (N) ←── role_guild (1)
  │              ↑ role_id 关联到 role_main
  │
  └──→ guild_apply (N)
  │
  └──→ guild_log (N)
```

---

## 3. 公会 Actor 生命周期

### 3.1 激活与加载

```go
type GuildInfo struct {
    *Guild
    Members []*GuildMember
}

type GuildActor struct {
    gxymodule.ModuleBase
    *gxyactor.ActorBase
    GuildID int64
    Data    *GuildInfo // 内存缓存
}

func (g *GuildActor) OnModStart(ctx context.Context) error {
    // 1. 从 DB 加载
    g.Data = &GuildInfo{Guild: &Guild{}}
    gxypgx.DB().First(g.Data.Guild, g.GuildID)
    gxypgx.DB().Where("guild_id = ?", g.GuildID).Find(&g.Data.Members)

    // 2. 注册定时器
    g.Timer().AddTick(ctx, PersistTick, g.TickSave)
    g.Timer().AddCron(ctx, gxytimer.DayRefresh, g.onDayRefresh)

    glog.Infof(ctx, "guild actor started, guildID=%d, members=%d",
        g.GuildID, len(g.Data.Members))
    return nil
}

func (g *GuildActor) OnModStop(ctx context.Context) error {
    // 关闭时做一次保存
    g.TickSave(ctx)
    glog.Infof(ctx, "guild actor stopped, guildID=%d", g.GuildID)
    return nil
}
```

### 3.2 持久化

```go
func (g *GuildActor) TickSave(ctx context.Context, _ ...gxytimer.TimerActiveInfo) {
    if g.Data == nil {
        return
    }
    gxypgx.DB().Save(g.Data.Guild)
}
```

### 3.3 每日定时器

```go
func (g *GuildActor) onDayRefresh(ctx context.Context, _ gxytimer.TimerActiveInfo) {
    // 1. 自动转让不活跃会长（30天未上线）
    if g.isLeaderInactive() {
        newLeader := g.findActiveViceLeader()
        if newLeader != 0 {
            g.transferLeader(ctx, g.Data.Guild.LeaderID, newLeader)
            g.addLog(ctx, fmt.Sprintf("会长 %d 因长期未上线自动转让给 %d",
                g.Data.Guild.LeaderID, newLeader))
            g.notifyMembers(ctx, &pb.NotifyGuildInfo{})
        }
    }

    // 2. 清理过期申请
    gxypgx.DB().Where("guild_id = ? AND status = 0 AND expire_at < NOW()",
        g.GuildID).Delete(&GuildApply{})
}
```

---

## 4. 写操作（Actor 消息）

所有写操作通过 actor 消息发给 guild actor，由 mailbox 串行化。

### 4.1 创建公会（走 HTTP）

创建公会属于初态操作（公会 ID 来自 DB 自增），走 HTTP handler，激活 guild actor 也在 handler 里完成。

**角色侧流程：**

```go
func (r *RoleGuild) ReqCreateGuild(ctx context.Context, req *pb.ReqCreateGuild) (*pb.RspCreateGuild, error) {
    // 1. 条件检查（角色侧）
    basic := r.Role.GetBasic()
    cfg := gamecfg.GetGuildConfig()
    if basic.Level < cfg.UnlockLevel {
        return nil, ErrLevelNotEnough
    }
    if r.GuildID > 0 {
        return nil, ErrAlreadyInGuild
    }
    if !r.Role.GetBag().CheckGoods(cfg.CreateCost) {
        return nil, ErrCostNotEnough
    }

    // 2. 先扣除消耗
    if err := r.Role.GetBag().SaveGoods(ctx, nil, cfg.CreateCost, "create_guild"); err != nil {
        return nil, err
    }

    // 3. HTTP 创建公会（handler 负责写入 DB + 激活 actor）
    rsp, err := callGuildCreate(ctx, req.Name, req.Declaration, req.Icon, req.NeedApproval)
    if err != nil {
        // 创建失败需要退款
        r.Role.GetBag().SaveGoods(ctx, cfg.CreateCost, nil, "create_guild_refund")
        return nil, err
    }

    // 4. 本地注册
    r.GuildID = rsp.GuildID
    chat.RegisterRoleGuildChat(r.RoleID, r.GuildID, r.Role.Self())

    return &pb.RspCreateGuild{GuildId: rsp.GuildID}, nil
}
```

**HTTP handler 处理：**

```go
func (h *GuildHandler) Create(ctx context.Context, req *CreateGuildReq) (any, error) {
    // 1. 检查名称唯一性
    var count int64
    gxypgx.DB().Model(&Guild{}).Where("name = ?", req.Name).Count(&count)
    if count > 0 {
        return nil, gxyhttp.NewErrCode(1, "公会名称已存在")
    }

    // 2. 写入公会记录
    guild := &Guild{
        Name: req.Name, Level: 1, LeaderID: req.LeaderID,
        Declaration: req.Declaration, Icon: req.Icon,
        NeedApproval: req.NeedApproval, MemberCount: 1,
    }
    gxypgx.DB().Create(guild)

    // 3. 写入成员
    gxypgx.DB().Create(&GuildMember{
        GuildID: guild.ID, RoleID: req.LeaderID,
        Position: PositionLeader, JoinedAt: time.Now().Unix(),
    })

    // 4. 更新 role_guild
    gxypgx.DB().Model(&RoleGuildState{}).
        Where("role_id = ?", req.LeaderID).Update("guild_id", guild.ID)

    // 5. 激活 guild actor（从 DB 加载数据到内存）
    lib.ActivateGuild(guild.ID)

    // 6. 写日志
    gxypgx.DB().Create(&GuildLog{GuildID: guild.ID, Content: "公会创建成功"})

    return map[string]int64{"guild_id": guild.ID}, nil
}
```

### 4.2 申请加入（需审核 → HTTP）

需审核时，申请只是写入 `guild_apply` 表，不涉及公会状态变更，走 HTTP：

```go
func (h *GuildHandler) ApplyGuild(ctx context.Context, req *ApplyGuildReq) (any, error) {
    // 检查玩家无公会
    var state RoleGuildState
    gxypgx.DB().First(&state, req.RoleID)
    if state.GuildID > 0 {
        return nil, gxyhttp.NewErrCode(1, "你已加入公会")
    }

    // 写入申请
    cfg := gamecfg.GetGuildConfig()
    gxypgx.DB().Create(&GuildApply{
        GuildID: req.GuildID, RoleID: req.RoleID,
        Status: 0, ExpireAt: time.Now().Add(time.Duration(cfg.ApplyExpireHours) * time.Hour),
    })
    return nil, nil
}
```

原子门在审批时做（见下方 4.3），同一个玩家申请多个公会最终只有第一个批准的生效。

### 4.3 直接加入（无需审核 → Actor 消息）

无需审核时，"加入"是修改公会成员列表的写操作，走 actor 消息：

```go
func (g *GuildActor) JoinGuild(ctx context.Context, req *pb.JoinGuildReq) error {
    // 1. 检查成员上限
    cfg := getLevelConfig(g.Data.Guild.Level)
    if len(g.Data.Members) >= int(cfg.MemberLimit) {
        return ErrGuildFull
    }

    // 2. 原子门：只有当前没公会的玩家才能加入
    result := gxypgx.DB().Model(&RoleGuildState{}).
        Where("role_id = ? AND guild_id = 0", req.RoleID).
        Update("guild_id", g.GuildID)
    if result.RowsAffected == 0 {
        return ErrPlayerAlreadyInGuild
    }

    // 3. 写入成员
    g.Data.Members = append(g.Data.Members, &GuildMember{
        GuildID: g.GuildID, RoleID: req.RoleID,
        Position: PositionMember, JoinedAt: time.Now().Unix(),
    })
    gxypgx.DB().Create(&GuildMember{GuildID: g.GuildID, RoleID: req.RoleID, Position: PositionMember})
    g.Data.Guild.MemberCount = int32(len(g.Data.Members))
    gxypgx.DB().Save(g.Data.Guild)

    // 4. 通知
    g.notifyPlayer(ctx, req.RoleID, &pb.NotifyGuildInfo{})
    g.notifyMembers(ctx, &pb.NotifyGuildInfo{}, req.RoleID)

    g.addLog(ctx, fmt.Sprintf("玩家 %d 加入公会", req.RoleID))
    return nil
}
```

### 4.4 审批加入

```go
func (g *GuildActor) ApproveApply(ctx context.Context, req *pb.ApproveApplyReq) error {
    // 1. 检查权限（会长/副会长）
    if !g.canApprove(req.OperatorID) {
        return ErrPermissionDenied
    }

    // 2. 检查成员上限
    cfg := getLevelConfig(g.Data.Guild.Level)
    if len(g.Data.Members) >= int(cfg.MemberLimit) {
        return ErrGuildFull
    }

    // 3. 原子门：只有当前没公会的玩家才能加入
    result := gxypgx.DB().Model(&RoleGuildState{}).
        Where("role_id = ? AND guild_id = 0", req.TargetID).
        Update("guild_id", g.GuildID)
    if result.RowsAffected == 0 {
        return ErrPlayerAlreadyInGuild
    }

    // 4. 写入成员
    g.Data.Members = append(g.Data.Members, &GuildMember{
        GuildID: g.GuildID, RoleID: req.TargetID,
        Position: PositionMember, JoinedAt: time.Now().Unix(),
    })
    gxypgx.DB().Create(&GuildMember{GuildID: g.GuildID, RoleID: req.TargetID, Position: PositionMember})
    g.Data.Guild.MemberCount = int32(len(g.Data.Members))

    // 5. 更新申请状态
    gxypgx.DB().Model(&GuildApply{}).
        Where("id = ?", req.ApplyID).Update("status", 1)

    // 6. 通知申请人 + 全体成员
    g.notifyPlayer(ctx, req.TargetID, &pb.NotifyGuildInfo{})
    g.notifyMembers(ctx, &pb.NotifyGuildInfo{}, req.TargetID)

    g.addLog(ctx, fmt.Sprintf("玩家 %d 加入公会", req.TargetID))
    return nil
}
```

### 4.5 踢出成员

```go
func (g *GuildActor) KickMember(ctx context.Context, req *pb.KickMemberReq) error {
    if !g.canKick(req.OperatorID, req.TargetID) {
        return ErrPermissionDenied
    }

    // 1. 从成员列表移除
    g.Data.Members = removeMember(g.Data.Members, req.TargetID)
    g.Data.Guild.MemberCount = int32(len(g.Data.Members))

    // 2. DB 删除
    gxypgx.DB().Delete(&GuildMember{}, "guild_id = ? AND role_id = ?", g.GuildID, req.TargetID)
    gxypgx.DB().Save(g.Data.Guild)

    // 3. 更新 role_guild（被踢者）
    gxypgx.DB().Model(&RoleGuildState{}).
        Where("role_id = ?", req.TargetID).Update("guild_id", 0)

    // 4. 通知
    g.notifyPlayer(ctx, req.TargetID, &pb.NotifyGuildKicked{Reason: req.Reason})
    g.notifyMembers(ctx, &pb.NotifyGuildInfo{}, req.TargetID)

    g.addLog(ctx, fmt.Sprintf("玩家 %d 被 %d 踢出公会", req.TargetID, req.OperatorID))
    return nil
}
```

### 4.6 其他写操作

| 操作 | Actor 消息 | 说明 |
|------|-----------|------|
| 拒绝申请 | `RejectApply` | 更新 apply status=2 |
| 会长转让 | `TransferLeader` | 双方 position 互换，通知全员 |
| 任命副会长 | `AppointViceLeader` | 检查上限，更新 position，通知全员 |
| 取消副会长 | `RemoveViceLeader` | 更新 position=3，通知全员 |
| 修改宣言/公告 | `UpdateGuildInfo` | 更新 DB，通知 `NotifyGuildBasic` |
| 修改审核开关 | `ToggleApproval` | 更新 `need_approval`，通知 `NotifyGuildBasic` |
| 退出公会 | `LeaveGuild` | 移出 member，更新 role_guild，通知全员 |
| 解散公会 | `DisbandGuild` | 必须只剩会长 1 人，清空 member + apply + log，通知全员 |

---

## 5. 通知协议

简化后共 3 个通知协议：

| 协议 | 触发场景 | 客户端行为 |
|------|---------|-----------|
| `NotifyGuildInfo` | 成员加入/退出/被踢、职位变动、会长转让、解散 | 重新拉取公会大厅（含成员列表） |
| `NotifyGuildBasic` | 宣言修改、公告修改、审核开关变更 | 重新拉取公会基本信息 |
| `NotifyGuildKicked` | 被踢出公会 | 弹出提示，跳转到公会列表界面 |

Role actor 接收后直接推客户端：

```go
func (r *RoleMain) HandleMessage(ctx context.Context, msg any) error {
    switch m := msg.(type) {
    case *pb.NotifyGuildInfo, *pb.NotifyGuildBasic, *pb.NotifyGuildKicked:
        r.SendClient(ctx, m.(proto.Message))
        return nil
    }
    ...
}
```

通知函数：

```go
// 通知单个玩家
func (g *GuildActor) notifyPlayer(ctx context.Context, roleID int64, msg proto.Message) {
    pid, err := lib.GetRoleActor(roleID, false)
    if err != nil || pid == nil {
        return // 不在线
    }
    g.Send(pid, msg)
}

// 通知全体成员（可选排除）
func (g *GuildActor) notifyMembers(ctx context.Context, msg proto.Message, exclude ...int64) {
    excludeSet := toSet(exclude)
    for _, m := range g.Data.Members {
        if excludeSet.Contains(m.RoleID) {
            continue
        }
        pid, err := lib.GetRoleActor(m.RoleID, false)
        if err != nil || pid == nil {
            continue
        }
        g.Send(pid, msg)
    }
}
```

---

## 6. 读操作（HTTP）

以下操作不修改状态，不需要激活 actor，直接由 HTTP handler 处理：

| 接口 | Path | 说明 |
|------|------|------|
| 创建公会 | `POST /create` | 创建公会记录并激活 actor |
| 申请加入（需审核） | `POST /apply` | 写入申请记录 |
| 搜索公会 | `POST /search` | 按名称模糊搜索或按 ID 精确搜索 |
| 公会大厅信息 | `POST /info` | 返回公会基本信息+成员列表（限成员） |
| 公会基本信息 | `POST /basic` | 返回公会基本信息（公开） |
| 申请列表 | `POST /apply_list` | 返回当前申请列表（限会长/副会长） |
| 公会日志 | `POST /log_list` | 返回公会日志 |

### 6.1 搜索公会

```go
type SearchGuildReq struct {
    g.Meta `path:"/search"`
    Keyword string `p:"keyword"` // 公会名称或 ID
}

func (h *GuildHandler) Search(ctx context.Context, req *SearchGuildReq) (any, error) {
    var guilds []Guild
    // 数字 → ID 精确搜索
    if id, err := strconv.ParseInt(req.Keyword, 10, 64); err == nil {
        gxypgx.DB().Where("id = ?", id).Find(&guilds)
    } else {
        // 文字 → 名称模糊搜索
        gxypgx.DB().Where("name LIKE ?", "%"+req.Keyword+"%").Limit(20).Find(&guilds)
    }
    return guilds, nil
}
```

### 6.2 公会大厅信息

```go
type GuildInfoReq struct {
    g.Meta `path:"/info"`
    GuildID int64 `p:"guild_id" v:"required"`
}

func (h *GuildHandler) Info(ctx context.Context, req *GuildInfoReq) (any, error) {
    var guild Guild
    if err := gxypgx.DB().First(&guild, req.GuildID).Error; err != nil {
        return nil, gxyhttp.NewErrCode(1, "公会不存在")
    }

    var members []GuildMember
    gxypgx.DB().Where("guild_id = ?", req.GuildID).Find(&members)

    return map[string]any{
        "guild":   guild,
        "members": members,
    }, nil
}
```

---

## 7. 公会聊天集成

### 7.1 新增注册机制

在 chat 模块中新增一个 `localGuildRoles` map，类似 `localRoles`：

```go
// chat/sidecar.go — 新增
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

### 7.2 注册时机

RoleGuild 子模块在初始化时注册：

```go
func (r *RoleGuild) OnModStart(ctx context.Context) error {
    if r.GuildID > 0 {
        chat.RegisterRoleGuildChat(r.RoleID, r.GuildID, r.Role.Self())
    }
    return nil
}

// 加入公会后
func (r *RoleGuild) OnGuildJoined(ctx context.Context, guildID int64) {
    r.GuildID = guildID
    chat.RegisterRoleGuildChat(r.RoleID, guildID, r.Role.Self())
}

// 退出/被踢/解散后
func (r *RoleGuild) OnGuildLeft(ctx context.Context) {
    chat.UnregisterRoleGuildChat(r.RoleID)
    r.GuildID = 0
}
```

### 7.3 Sidecar 路由

```go
// chat/sidecar.go — 新增路由
func handleSidecarMsg(ctx context.Context, msg *redis.Message) {
    channel := msg.Channel

    switch {
    case strings.HasPrefix(channel, "chat:pub:lobby:"):
        // ... 已有逻辑 ...

    case channel == "chat:pub:system":
        // ... 已有逻辑 ...

    case strings.HasPrefix(channel, "chat:pub:private:"):
        // ... 已有逻辑 ...

    case strings.HasPrefix(channel, "chat:pub:guild:"):
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
    }
}
```

### 7.4 公会聊天 API

在 chat handler 中新增两个接口：

| 接口 | Path | 说明 |
|------|------|------|
| 发送公会聊天 | `POST /send_guild` | 验证成员身份后发布到 `chat:pub:guild:{guild_id}` |
| 公会聊天历史 | `POST /guild_history` | 从 Redis list 获取最近记录 |

消息存储复用世界频道的 Redis list 模式，key 为 `chat:msg:guild:{guild_id}`。

---

## 8. RoleGuild 子模块

### 8.1 结构定义

```go
type RoleGuildState struct {
    RolePersistState
}

func (RoleGuildState) TableName() string { return "role_guild" }

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
```

### 8.2 嵌入 roleModules

```go
// role_main.go
type roleModules struct {
    Bag           *RoleBag
    Basic         *RoleBasic
    Public        *RolePublic
    Extra         *RoleExtra
    Flower        *RoleFlower
    Plot          *RolePlot
    Steal         *RoleSteal
    MainTask      *RoleMainTask
    ResidentOrder *RoleResidentOrder
    GM            *RoleGM
    Chat          *RoleChat
    Guild         *RoleGuild       // ← 新增
}
```

### 8.3 Proto Handler 示例（写操作 → Actor 消息 + 读操作 → HTTP）

```go
// role_guild.go

// 创建公会 → HTTP（先扣消耗 → HTTP 创建 → 激活 actor）
func (r *RoleGuild) ReqCreateGuild(ctx context.Context, req *pb.ReqCreateGuild) (*pb.RspCreateGuild, error) {
    basic := r.Role.GetBasic()
    cfg := gamecfg.GetGuildConfig()
    if basic.Level < cfg.UnlockLevel {
        return nil, ErrLevelNotEnough
    }
    if r.GuildID > 0 {
        return nil, ErrAlreadyInGuild
    }
    // 先扣消耗
    if err := r.Role.GetBag().SaveGoods(ctx, nil, cfg.CreateCost, "create_guild"); err != nil {
        return nil, err
    }
    // HTTP 创建
    rsp, err := callGuildCreate(ctx, req.Name, req.Declaration, req.Icon, req.NeedApproval)
    if err != nil {
        r.Role.GetBag().SaveGoods(ctx, cfg.CreateCost, nil, "create_guild_refund")
        return nil, err
    }
    r.GuildID = rsp.GuildID
    chat.RegisterRoleGuildChat(r.RoleID, r.GuildID, r.Role.Self())
    return &pb.RspCreateGuild{GuildId: rsp.GuildID}, nil
}

// 审批加入 → Actor 消息（需要 mailbox 串行化）
func (r *RoleGuild) ReqApproveApply(ctx context.Context, req *pb.ReqApproveApply) (*pb.RspApproveApply, error) {
    pid, err := lib.ActivateGuild(req.GuildId)
    if err != nil {
        return nil, err
    }
    rsp, err := gxyactor.Request(pid, req)
    if err != nil {
        return nil, err
    }
    return rsp.(*pb.RspApproveApply), nil
}

// 搜索公会 → HTTP 只读
func (r *RoleGuild) ReqSearchGuild(ctx context.Context, req *pb.ReqSearchGuild) (*pb.RspSearchGuild, error) {
    rsp, err := gxyhttp.HttpSystem().PostService(ctx, "guild",
        fmt.Sprintf("search?keyword=%s", url.QueryEscape(req.Keyword)))
    if err != nil {
        return nil, err
    }
    // parse rsp ...
}
```

---

## 9. 配置集成

已有自动生成的配置表可以直接使用：

| 配置结构体 | 用途 |
|-----------|------|
| `gamecfg.GardenGuildConfig` | 解锁等级、创建消耗、字数限制、申请过期时间等 |
| `gamecfg.GardenGuildLevel` | 等级对应的成员上限、副会长上限 |
| `gamecfg.GardenGuildPosition` | 各职位的权限配置 |
| `gamecfg.GardenGuildChat` | 聊天字数上限、冷却、历史保留数量 |

---

## 10. 目录结构（最终）

```
src/apps/guild/
├── guild_app.go          # App 注册、OnModInit、AutoMigrate
├── guild_service.go       # HTTP service 启动（只读接口）
├── logic/
│   ├── model.go           # GORM 模型
│   ├── schema.go          # AutoMigrate 调用
│   ├── guild.go           # 核心业务逻辑函数
│   ├── guild_actor.go     # 公会 actor（生命周期、Timer、写操作）
│   └── handler.go         # HTTP handler（只读操作）

src/apps/role/internal/logic/
├── role_guild.go          # RoleGuild 子模块

src/apps/chat/
├── sidecar.go             # + 公会频道路由
├── redis.go               # + 公会聊天存储/发布
├── handler.go             # + 公会聊天 API
```

---

## 11. 已知待解决问题（后续迭代）

- 公会等级成长（首版默认 1 级，配置取 `GardenGuildLevel[1]`）
- 公会财富展示（首版展示位保留但显示 0）
- 后续入口预留（建设、商店、种植等只显示"功能暂未开放"）
