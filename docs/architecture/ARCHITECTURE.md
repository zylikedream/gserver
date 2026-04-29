# Architecture

## 设计哲学

本项目采用 **Actor 模型** 构建分布式游戏服务器，核心灵感来自 Erlang/OTP 和微软 Orleans 的虚拟 Actor 架构。

**核心原则：**
- 每个玩家角色是一个虚拟 Actor，通过唯一 ID 标识
- Actor 可以在集群任意节点上激活，通过 Redis 实现位置透明
- 所有状态变更在单个 Actor 内串行处理，天然避免并发冲突
- 模块化设计：框架层与业务层严格分离

## 架构分层

```
┌─────────────────────────────────────────────────────────┐
│                    Node (进程入口)                        │
│  node/main.go — 读取配置，组装模块树，管理生命周期          │
├─────────────────────────────────────────────────────────┤
│                    App (应用层)                           │
│  gateway  — 网关服务（连接管理、消息路由）                   │
│  role     — 角色服务（Actor 注册、业务逻辑）                │
│  redis    — Redis 客户端模块                              │
│  pgx      — PostgreSQL 客户端模块 (GORM)                  │
│  mq       — 消息队列模块                                  │
│  service  — 服务注册/发现模块                              │
│  http     — HTTP 服务模块                                 │
│  actor    — Actor 系统模块                                │
├─────────────────────────────────────────────────────────┤
│                    Core (框架层)                          │
│  gxyactor   — Actor 系统（ActorBase, 定位器, Activator）  │
│  gxymodule  — 模块系统（树状结构、生命周期管理）             │
│  gxynet     — 网络层（gnet v2 TCP server, LTPV codec）     │
│  gxypgx     — PostgreSQL 封装 (GORM, AutoMigrate)        │
│  gxyredis   — Redis 封装                                 │
│  gxytimer   — 定时器系统                                  │
│  gxylocator — 分布式定位器（Lua 脚本原子操作）              │
│  gxyservice — 服务注册/发现                               │
│  gxyregistery— 注册中心实现（Consul / etcd）               │
│  gxyhttp    — HTTP 服务                                   │
│  gxylog     — 日志系统                                    │
│  gxymq      — 消息队列（Redis Pub/Sub / Pulsar）          │
│  gxyutil    — 通用工具（消息路由、反射、配置）              │
│  gxynode    — 节点模块（App 注册、依赖加载）               │
├─────────────────────────────────────────────────────────┤
│                    Protocol (协议层)                      │
│  pb/ — Protobuf 消息定义                                  │
│  message.go — 消息类型常量                                │
└─────────────────────────────────────────────────────────┘
```

## Actor 系统 (核心)

### 类型层次

```
actor.Actor (protoactor-go 接口)
    ↓
IActor (gxyactor 接口)
  - Init(ctx, args) error
  - DelayInit(ctx) error
  - Terminate(ctx, err)
  - HandleMessage(ctx, msg) — 应用层消息处理
  - Timer() *ActorTimer
  - Self() PID
    ↓
ActorBase (基础实现)
  - 消息接收分发 (doReceive)
  - Send/Call/LocalSend 消息发送
  - 定时器管理 (ActorTimer)
  - MsgHandler 反射路由 (AutoHandleMsg)
  - Supervisor 策略 (OneForOne, StopDirective)
```

### 虚拟 Actor 生命周期

```
1. ActivateActor(kind, id) 被调用
2. activatorManager 查询 Redis Locator:
   a. 找到 → 返回已有 PID
   b. 未找到 → 通过 Service Registry 选择节点
3. 向目标节点的 activatorRouter 发送 ActorActive 消息
4. activatorRouter 包装为 hashableActorActive 转发到一致性哈希池
5. actorActivator 处理激活:
   a. SpawnNamed (带 ContextDecorator 传递 actorID)
   b. SETNX 原子注册 PID 到 Redis Locator (TTL 40s)
   c. 注册失败 → StopActor，防止重复激活
   d. 返回 PID
6. Actor 接收消息处理业务逻辑
7. 定时续约 (30s 间隔，Lua 批量 SETEX)
8. Actor 停止时:
   a. 接收 Terminated 通知
   b. Lua 脚本条件注销（校验值匹配后删除）
   c. 从 actorActivator 的 childs map 和 ActorMgr 移除
   d. 触发 Terminate 回调（保存数据）
```

### Actor 路由架构

每种 Actor 类型注册时创建两级路由：
- **activatorRouter** — 外部入口，接收远程 `ActorActive` 消息
- **ConsistentHashPool (5实例)** — 内部池，`actorActivator` 按 ID 哈希分发

```
Remote Node → activatorRouter → hashableActorActive → ConsistentHashPool → actorActivator → spawn actor
```

## 模块系统 (gxymodule)

### 树状结构

```
ModuleBase
  ├── self   — 自身 IModule 引用
  ├── parent — 父模块引用
  └── childs — 子模块列表

IModule 接口:
  - GetModName() string
  - GetModID() string
  - OnModInit(ctx) error       // 初始化
  - OnModStart(ctx) error      // 启动
  - OnModStartAfter(ctx) error // 启动后（依赖其他模块时使用）
  - OnModStop(ctx) error       // 停止
  - OnModStopBefore(ctx) error // 停止前
```

### 生命周期

```
Init 阶段（深度优先，从父到子）:
  parent.OnModInit → child1.OnModInit → child2.OnModInit

Start 阶段:
  parent.OnModStart → child1.OnModStart → child2.OnModStart
  → child1.OnModStartAfter → child2.OnModStartAfter

Stop 阶段（逆序）:
  child2.OnModStop → child1.OnModStop → parent.OnModStop
```

## 消息流转

### 客户端消息流

```
Client (TCP)
  → gxynet (gnet v2 endpoint 接收)
  → GateHandler.OnMessage (LTPV 解包)
  → Session Actor (LocalSend, 不经过序列化)
  → Session.OnHandleClientMessage
    → handshake: 本地处理，ActivateRole(roleID)
    → data: CallSync(RoleActor, ClientMsg{id, anypb.Msg})
  → RoleMain Actor (远程或本地)
  → RoleMain.HandleClientMsg
    → anypb.UnmarshalNew 解包
    → MsgHandler.CallWithMsg 反射路由到对应模块方法
    → 返回 ServerMsg{anypb.Msg}
  → Session.OnHandleServerMessage
  → endpoint.SendMsg → Client
```

### 消息类型

| 类型 | 用途 | 序列化 |
|------|------|--------|
| `ClientMsg` | Session → Role 请求 | anypb 包装 |
| `ServerMsg` | Role → Session 响应 | anypb 包装 |
| `ActorActive` | 激活虚拟 Actor | protobuf |
| `ActorStop` | 停止 Actor | protobuf |
| `ActorError` | Actor 错误响应 | protobuf |
| 本地消息 (any) | 同节点内通信 | 无序列化 |

## 数据持久化模型

### 持久化状态接口

```go
IPersistState interface {
    SetRoleID(roleID int64)
    GetUpdateAt() time.Time / SetUpdateAt(time.Time)
    GetIndexes() []string
    MarkDirty()
    IsDirty() bool
    ClearDirty()
}
```

### RolePersistState (基类)

```go
RolePersistState struct {
    RoleID   int64     `gorm:"column:role_id;primaryKey"`
    UpdateAt time.Time `gorm:"column:update_at;autoUpdateTime"`
    dirty    bool
}
```

- `gorm:"column:snake_case"` 标签：PostgreSQL 列映射
- `gorm:"type:jsonb"` 标签：Map/Slice 字段自动序列化为 JSONB
- 自定义 `Value()`/`Scan()` 方法处理复杂类型（如 `GoodsMap`）

### 保存策略

1. **脏标记**: 模块修改数据后调用 `MarkDirty()`，保存时检查 `IsDirty()`
2. **GORM Save**: `db.Save(modState)` 自动判断 INSERT/UPDATE
3. **定时保存**: 600s 间隔 Tick
4. **强制保存**: 建号、登出、Actor 停止时
5. **Schema 迁移**: `db.AutoMigrate()` 启动时自动创建/更新表结构

## 服务发现（双层）

### Layer 1: Service Registry
- **作用**: 服务节点注册与发现
- **后端**: Consul（默认）或 etcd
- **选择**: 一致性哈希 (ConsistentHashSelector)
- **流程**: ServiceApp.OnModStartAfter → 注册所有 IService

### Layer 2: Actor Locator
- **作用**: Actor 实例的精确位置
- **存储**: Redis KV (SETNX 原子注册, TTL 40s)
- **流程**: Actor spawn 时 SETNX 注册，停止时 Lua 条件注销，30s Lua 批量续约

### 查找流程

```
ActivateActor(kind, id)
  → Redis Locator 查找 → 找到 → 返回 PID
  → 未找到 → Service Registry 选择节点 → spawnActor → SETNX 注册 Locator → 返回 PID
```

## 角色内部架构

```
RoleMain (Actor + Module容器)
  ├── RoleBasic      — 基础信息 (名称、头像、登录时间)
  ├── RoleBag        — 背包系统 (物品、货币)
  ├── RolePublic     — 公开信息 (用于排行榜等，带 Redis 缓存)
  └── RoleExtra      — 扩展数据 (Cron 定时器状态)

每个模块:
  - 实现 IRoleModule 接口
  - 继承 RoleModule (内嵌 ModuleBase)
  - 持有独立的 PersistState (独立 PostgreSQL table)
  - 通过 MsgHandler 自动路由 protobuf 消息到方法
  - 通过 EventBus 发布/订阅内部事件
  - 数据修改后调用 MarkDirty() 标记需要保存
```

---
*Last updated: 2026-04-29*
