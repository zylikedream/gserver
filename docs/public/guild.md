# 公会系统

## 概述

公会系统采用 **Guild Actor** 架构。每个公会对应一个 Actor（按 guildID 命名），通过 consistent-hash 路由到固定节点。公会数据主表使用 JSONB 列存储成员、申请和日志，`role_guild` 表记录角色与公会的从属关系。

## 架构

```
┌──────────────────────────────────────────────────────────┐
│  Guild HTTP 服务 (guild_http_service.go)                  │
│  - /create (创建公会，HTTP → GuildActor Call)             │
│  - /search (搜索公会，直接查 DB)                          │
└──────────────────────┬───────────────────────────────────┘
                       │ HTTP (PostService)
         ┌─────────────┴─────────────┐
         │  RoleGuild 模块           │
         │  (role_guild.go)         │
         │  - ReqCreateGuild        │
         │  - ReqApplyGuild         │
         │  - withGuildActor → Call │
         └─────────────┬─────────────┘
                       │ Actor Call
         ┌─────────────┴──────────────────────────┐
         │  GuildActor (consistent-hash)           │
         │  - Guild 数据（含 JSONB 成员/申请/日志）│
         │  - TickSave 600s                       │
         │  - DayRefresh cron                     │
         │  - AutoHandleMsg 反射派发              │
         └────────────────────────────────────────┘
```

## GuildActor 生命周期

```
ActivateActor(guildID)
         │
    Init(args=[guildID])
         │
    DelayInit
         ├── DB.First → 加载 Guild 数据
         ├── AddTick(guild_save, 600s)
         └── AddCron(DayRefresh)
         │
    HandleMessage
         ├── 业务消息 → AutoHandleMsg 反射派发
         └── Timer → save / onDayRefresh
         │
    Terminate → StopModule
```

公会 Actor 没有空闲自动清理逻辑（与 ChannelActor 不同），因为公会数据在 Actor 内存中管理，Actor 退出不会导致数据丢失（已持久化到 DB）。

## 数据模型

### Guild 主表（JSONB 列）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int64 PK | 公会ID |
| name | string(32) | 公会名（唯一索引）|
| level | int32 | 公会等级 |
| icon | string | 图标 |
| declaration | string(200) | 宣言 |
| announcement | string(500) | 公告 |
| need_approval | bool | 是否需要审批加入 |
| member_count | int32 | 成员数 |
| leader_id | int64 | 会长角色ID |
| members | []GuildMember (JSONB) | 成员列表 |
| apply_list | []GuildApply (JSONB) | 申请列表 |
| logs | []GuildLog (JSONB) | 日志列表（最多 100 条）|
| version | int64 | 乐观锁版本号 |
| created_at | timestamp | 创建时间 |
| updated_at | timestamp | 更新时间 |

### role_guild 表

| 字段 | 类型 | 说明 |
|------|------|------|
| role_id | int64 PK | 角色ID |
| guild_id | int64 | 所属公会ID（0=无公会）|

独立表而非 JSONB 嵌入，供 GuildActor 原子操作（`addMember` / `removeMember` 时原子门修改角色所属公会，防止多公会竞态）。

### 内嵌结构

**GuildMember**（Position: 1=会长 2=副会长 3=成员）

| 字段 | 类型 | 说明 |
|------|------|------|
| role_id | int64 | 角色ID |
| position | int32 | 职位 |
| joined_at | int64 | 加入时间戳 |

PRolePublic 不存储，由 `GetRolePublic` 动态填充到通知消息。

**GuildApply**

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int64 | 申请ID（自增）|
| role_id | int64 | 申请角色ID |
| status | int32 | 0=待处理 1=同意 2=拒绝 |
| created_at | timestamp | 申请时间 |
| expire_at | timestamp | 过期时间 |

**GuildLog**

| 字段 | 类型 | 说明 |
|------|------|------|
| content | string | 日志内容 |
| created_at | timestamp | 创建时间 |

## 消息流

### 创建公会

1. Client → `ReqCreateGuild` → RoleGuild 模块
2. 校验等级、消耗、是否已有公会
3. HTTP 调用 Guild 服务 `/create` → GuildActor `ActorCreateGuild`
4. GuildActor 创建 Guild 记录，`addMember` 设置会长为创建者
5. 扣减创建消耗，设置 `RoleGuild.GuildID`
6. 返回 guildID

### 申请加入

1. Client → `ReqApplyGuild` → RoleGuild 模块
2. `lib.GetGuildActor(req.GuildId)` 获取 GuildActor PID
3. `Call(pid, ReqApplyGuild)` → GuildActor `ApplyGuild`
4. 免审批 → `joinDirect`（直接加入，`addMember`）
5. 需审批 → `createApply`（创建申请，通知会长/副会长）
6. GuildID 由 `NotifyGuildInfo` 推送更新

### 通用操作（信息/日志/审批/踢出/职位/转让/修改/退出/解散）

```
RoleGuild 模块 → withGuildActor(ctx, req)
                         │
                  lib.GetGuildActor(guildID)
                         │
                  Call(pid, req, 10s)
                         │
                  GuildActor 反射派发到对应 Handler
```

## Proto 接口

ID 段 `29001~29099`，文件 `protocol/client/guild.proto`。

| 消息 | ID | 方向 | 说明 |
|------|----|------|------|
| ReqCreateGuild / RspCreateGuild | 29001-29002 | C→S / S→C | 创建公会 |
| ReqSearchGuild / RspSearchGuild | 29003-29004 | C→S / S→C | 搜索公会 |
| ReqGuildInfo / RspGuildInfo | 29005-29006 | C→S / S→C | 公会信息 |
| ReqApplyGuild / RspApplyGuild | 29007-29008 | C→S / S→C | 申请加入 |
| ReqGuildLogs / RspGuildLogs | 29009-29010 | C→S / S→C | 公会日志 |
| ReqGuildApplyList / RspGuildApplyList | 29011-29012 | C→S / S→C | 申请列表 |
| ReqApproveApply / RspApproveApply | 29013-29014 | C→S / S→C | 审批加入 |
| ReqKickMember / RspKickMember | 29015-29016 | C→S / S→C | 踢出成员 |
| ReqSetPosition / RspSetPosition | 29017-29018 | C→S / S→C | 设置职位 |
| ReqTransferLeader / RspTransferLeader | 29019-29020 | C→S / S→C | 转让会长 |
| ReqUpdateGuildInfo / RspUpdateGuildInfo | 29021-29022 | C→S / S→C | 修改公会信息 |
| ReqLeaveGuild / RspLeaveGuild | 29023-29024 | C→S / S→C | 退出公会 |
| ReqDisbandGuild / RspDisbandGuild | 29025-29026 | C→S / S→C | 解散公会 |
| NotifyGuildInfo | 29031 | S→C | 公会信息推送 |
| NotifyGuildBasic | 29032 | S→C | 公会基础信息推送 |
| NotifyGuildApply | 29033 | S→C | 新申请通知 |

## HTTP 接口

Guild HTTP 服务（`guild-http`），所有 POST。

| 路径 | 参数 | 说明 |
|------|------|------|
| /create | role_id, name, declaration, icon, need_approval | 创建公会 |
| /search | keyword | 搜索公会 |

## 通知推送

GuildActor 使用多种通知机制：

- **notifyPlayer(roleID, msg)**：直接发送给指定角色（`lib.GetRoleActor → Send`）
- **notifyGuildInfo**：广播给所有在线成员（遍历 `Data.Members`，逐个 `notifyPlayer`）
- **notifyGuildBasic**：广播公会基础信息变更
- **notifyApplyUpdate**：通知会长/副会长有新的申请

## 服务注册

- `guildService` (`guild_service.go`): `ServiceName()` = `lib.GUILD_ACTOR_TYPE`（`"guild"`），注册 GuildActor kind
- `guildHttpService` (`guild_http_service.go`): `ServiceName()` = `"guild-http"`，启动 HTTP 服务

## 配置

公会配置从 `GameConfig.TbGuildConfig` 读取：

| 参数 | 说明 |
|------|------|
| UnlockLevel | 创建公会所需等级 |
| CreateCost | 创建公会消耗（物品） |
| MemberLimit | 成员上限（等级相关）|

## 核心文件

| 文件 | 说明 |
|------|------|
| src/apps/guild/guild_app.go | App 注册 + schema 初始化 |
| src/apps/guild/guild_service.go | GuildActor kind 注册 |
| src/apps/guild/guild_http_service.go | HTTP 服务生命周期 |
| src/apps/guild/logic/guild_actor.go | GuildActor（数据、成员、申请、日志）|
| src/apps/guild/logic/handler.go | HTTP 路由处理（创建/搜索）|
| src/apps/guild/logic/model.go | Guild / GuildMember / GuildApply / GuildLog 模型 |
| src/apps/guild/logic/schema.go | DB 自动迁移 |
| src/apps/role/internal/logic/role_guild.go | Role Guild 子模块 |
