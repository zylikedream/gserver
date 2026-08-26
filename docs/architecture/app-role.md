# Role 玩家角色系统

Role App 是 GServer 最核心的业务系统，每个在线玩家对应一个 Role Actor（Grain）。

## 架构

```
┌──────────────────────────────────────────────────┐
│                 RoleService                       │
│  注册 actor kind "role"，提供服务发现               │
├──────────────────────────────────────────────────┤
│                   RoleMain                        │
│           Actor 入口，状态机，消息分发               │
├──────────┬──────────┬──────────┬─────────────────┤
│ RoleBasic│ RoleBag  │ RoleExtra│ RolePublic  ...  │
│ (基础信息)│ (背包)   │ (扩展)   │ (公开信息)        │
├──────────┴──────────┴──────────┴─────────────────┤
│              EventBus（进程内事件总线）              │
└──────────────────────────────────────────────────┘
```

## RoleService（`role_service.go`）

```go
func (r *roleService) OnModStart(ctx context.Context) error {
    gxyactor.RegisterActorKind("role", func() gxyactor.IActor {
        return logic.NewRoleMain()
    })
    return nil
}
```

- 服务名: `"role"`
- 权重: `GetActorCount("role")` — 当前节点上的角色数
- 注册 `RegisterActorKind("role", ...)` 使 Activator 系统可创建 Role Actor

## RoleMain Actor（`role_main.go`）

玩家角色的 Actor 实现。

### 生命周期

```
Init → DelayInit → (消息处理) → TickSave(600s) → Terminate
      │              │
      └── initRole   └── HandleClientMsg / HandleMessage
```

### 状态机

```go
type RoleState int32
```

- 通过 `canHandleMsg(state, msg)` 控制消息可达性
- 状态变化驱动可处理的消息范围

### 子模块系统

Role 使用组合模式管理多个子模块（`RoleModule`）：

| 模块 | 结构 | 说明 |
|------|------|------|
| Basic / Public / Extra | `RoleBasic` / `RolePublic` / `RoleExtra` | 基础、公开与扩展状态 |
| Bag | `RoleBag` | 背包（详见 [bag.md](../public/system/bag.md)） |
| Flower / Plot / Steal | `RoleFlower` / `RolePlot` / `RoleSteal` | 鲜花、种植与偷花 |
| MainTask / ResidentOrder | `RoleMainTask` / `RoleResidentOrder` | 主线任务与居民订单 |
| Chat / Guild / Friend | `RoleChat` / `RoleGuild` / `RoleFriend` | 外部业务服务适配 |
| Mail | `RoleMail` | 个人邮件、系统邮件与红点状态 |
| GM | `RoleGM` | GM 协议命令 |

### IRoleModule 接口

```go
type IRoleModule interface {
    gxymodule.IModule
    SetRole(role *RoleMain)
    OnCreate(ctx context.Context)
    AfterLogin(ctx context.Context)
    BeforeLogout(ctx context.Context)
    PersistState() IPersistState
}
```

### 持久化接口（IPersistState）

```go
type IPersistState interface {
    SetRoleID(roleID int64)
    GetUpdateAt() time.Time
    SetUpdateAt(updateAt time.Time)
    GetIndexes() []string
    MarkDirty()
    IsDirty() bool
    ClearDirty()
    GetVersion() int64
    SetVersion(v int64)
}
```

- 脏跟踪（Dirty Flag） → 定时刷盘
- 版本号（`Version`） → 乐观锁（详见下文）

## 持久化

### 初始化

`role_schema.go` 在 App 初始化时调用 GORM `AutoMigrate` 创建所有子模块表。

### 定时保存（TickSave）

- 周期：600 秒
- 遍历所有子模块，检查 `IsDirty()`
- 脏数据通过 `save()` 写入数据库

### save() 并发安全

两层保护：

1. **Redis 所有权检查**：写入前校验 Redis 中该 Actor 的 locate key 是否指向本节点（防止 Actor 迁移后旧节点回写）
2. **版本乐观锁**：`UPDATE ... WHERE version = oldVersion`，更新成功后 `version++`

首次写入（GORM `RowsAffected == 0`）时自动创建新行。

### 数据库表

| 表名 | 结构体 | 说明 |
|------|--------|------|
| `role_basic` / `role_public` / `role_extra` | 对应 Role 状态 | 基础、公开与扩展状态 |
| `role_bag` | `RoleBagState` | 背包（goods JSONB） |
| `role_flower` / `role_plot` / `role_steal` | 对应 Role 状态 | 鲜花、种植与偷花 |
| `role_main_task` / `role_resident_order` | 对应 Role 状态 | 主线任务与居民订单 |
| `role_chat` | `RoleChatState` | 聊天游标状态 |
| `role_mail_state` | `RoleMailState` | 玩家邮件游标与红点状态 |
| `steal_record` / `personal_mail` / `sys_mail` | 独立记录模型 | 偷花记录、个人邮件与系统邮件 |

## 消息处理

### 客户端消息

```
客户端 → Gateway → Session → RoleMain.HandleClientMsg
  → 反射路由到子模块方法（如 RoleBag.ReqBagInfo）
  → 响应 → Session → 客户端
```

### 消息注册

- `initMsgHandler()` 注册消息到 `gxyutil.MsgHandler`
- 子模块通过 `RoleMain.AddMsgHandler(module)` 自动注册其 proto handler 方法

## 事件系统

`src/apps/role/internal/event/` 提供进程内事件总线：

```go
bus := event.NewEventBus()
ref := bus.Subscribe(eventType, handler)
bus.Publish(eventType, data)
bus.Unsubscribe(eventType, ref)
```

## 生命周期流程

### 角色登录

```
客户端握手 → GetRoleIDByAccount → ActivateRole
  → Activator 创建/找到 RoleMain Actor
  → RoleMain.Init → initRole (加载模块数据)
  → DelayInit → initTimer + initMsgHandler
  → ReqAccountLogin → afterRoleLogin → 返回 RspAccountLogin
```

### 角色创建

```
ReqAccountCreate → RoleMain.OnRoleCreated
  → 调用每个子模块的 OnCreate
  → 发放初始物品
  → save()
```

### 角色登出

```
ReqAccountLogout → dologout → save + Terminate
  → 关闭 Session → Actor 停止
```

## 源码位置

| 文件 | 说明 |
|------|------|
| `src/apps/role/role_app.go` | Role App 定义 |
| `src/apps/role/role_service.go` | Actor kind 注册 |
| `src/apps/role/internal/logic/role_main.go` | RoleMain Actor 主逻辑 |
| `src/apps/role/internal/logic/role_module.go` | 子模块接口定义 |
| `src/apps/role/internal/logic/role_schema.go` | 数据库表初始化 |
| `src/apps/role/internal/logic/role_basic.go` | 基础信息模块 |
| `src/apps/role/internal/logic/role_bag.go` | 背包模块 |
| `src/apps/role/internal/logic/account_lookup.go` | 账号与角色关系反查 |
| `src/apps/role/internal/logic/role_extra.go` | 扩展模块 |
| `src/apps/role/internal/logic/role_public.go` | 公开信息模块 |
| `src/apps/role/internal/logic/role_flower.go` | 鲜花模块 |
| `src/apps/role/internal/logic/role_plot.go` | 种植模块 |
| `src/apps/role/internal/logic/role_gm.go` | GM 指令模块 |
| `src/apps/role/internal/event/` | 事件总线 |
